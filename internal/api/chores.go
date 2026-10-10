package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/liftedkilt/openchore/internal/discord"
	"github.com/liftedkilt/openchore/internal/llm"
	"github.com/liftedkilt/openchore/internal/model"
	"github.com/liftedkilt/openchore/internal/store"
	"github.com/liftedkilt/openchore/internal/tts"
	"github.com/liftedkilt/openchore/internal/webhook"
)

type ChoreHandler struct {
	store      *store.Store
	dispatcher *webhook.Dispatcher
	discord    *discord.Notifier
	aiSvc      *AIServices    // optional AI and read-aloud audio
	reviews    sync.WaitGroup // in-flight background photo reviews
}

func NewChoreHandler(s *store.Store, d *webhook.Dispatcher, dn *discord.Notifier, ai *AIServices) *ChoreHandler {
	return &ChoreHandler{store: s, dispatcher: d, discord: dn, aiSvc: ai}
}

// AIServices returns the handler's (swappable) AI and audio services.
func (h *ChoreHandler) AIServices() *AIServices { return h.aiSvc }

// SetAI replaces the AI client and read-aloud audio directly (tests).
// Either may be nil.
func (h *ChoreHandler) SetAI(ai *llm.Client, audio *tts.ChoreAudio) {
	h.aiSvc.set(ai, audio)
}

// WaitForReviews blocks until background photo reviews finish (for tests
// and shutdown).
func (h *ChoreHandler) WaitForReviews() {
	h.reviews.Wait()
}

func (h *ChoreHandler) List(w http.ResponseWriter, r *http.Request) {
	chores, err := h.store.ListChores(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chores")
		return
	}
	if chores == nil {
		chores = []model.Chore{}
	}
	writeJSON(w, http.StatusOK, chores)
}

func (h *ChoreHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid chore id")
		return
	}
	chore, err := h.store.GetChore(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get chore")
		return
	}
	if chore == nil {
		writeError(w, http.StatusNotFound, "chore not found")
		return
	}
	writeJSON(w, http.StatusOK, chore)
}

type createChoreRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Icon        string `json:"icon"`
	// PointsValue and MissedPenaltyValue are pointers so we can distinguish
	// "field omitted" (nil) from "field explicitly set to 0". Without this,
	// admins can't clear a penalty or zero out a point value via the UI.
	PointsValue        *int   `json:"points_value"`
	MissedPenaltyValue *int   `json:"missed_penalty_value"`
	EstimatedMinutes   *int   `json:"estimated_minutes"`
	RequiresApproval   bool   `json:"requires_approval"`
	RequiresPhoto      bool   `json:"requires_photo"`
	PhotoSource        string `json:"photo_source"`
}

func (h *ChoreHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createChoreRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.Category == "" {
		req.Category = model.CategoryCore
	}
	if req.Category != model.CategoryRequired && req.Category != model.CategoryCore && req.Category != model.CategoryBonus {
		writeError(w, http.StatusBadRequest, "category must be required, core, or bonus")
		return
	}
	if req.PointsValue != nil && *req.PointsValue < 0 {
		writeError(w, http.StatusBadRequest, "points_value must be non-negative")
		return
	}
	if req.MissedPenaltyValue != nil && *req.MissedPenaltyValue < 0 {
		writeError(w, http.StatusBadRequest, "missed_penalty_value must be non-negative")
		return
	}
	if req.EstimatedMinutes != nil && *req.EstimatedMinutes < 0 {
		writeError(w, http.StatusBadRequest, "estimated_minutes must be non-negative")
		return
	}

	photoSource := req.PhotoSource
	if photoSource == "" {
		photoSource = model.PhotoSourceChild
	}
	if photoSource != model.PhotoSourceChild && photoSource != model.PhotoSourceExternal && photoSource != model.PhotoSourceBoth {
		writeError(w, http.StatusBadRequest, "photo_source must be child, external, or both")
		return
	}

	user := UserFromContext(r.Context())
	chore := &model.Chore{
		Title:              req.Title,
		Description:        req.Description,
		Category:           req.Category,
		Icon:               req.Icon,
		PointsValue:        intPtrOrZero(req.PointsValue),
		MissedPenaltyValue: intPtrOrZero(req.MissedPenaltyValue),
		EstimatedMinutes:   req.EstimatedMinutes,
		RequiresApproval:   req.RequiresApproval,
		RequiresPhoto:      req.RequiresPhoto,
		PhotoSource:        photoSource,
		Source:             "manual",
		CreatedBy:          user.ID,
	}
	if err := h.store.CreateChore(r.Context(), chore); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create chore")
		return
	}

	if audio := h.aiSvc.Audio(); audio != nil {
		audio.GenerateAsync(*chore)
	}

	writeJSON(w, http.StatusCreated, chore)
}

func (h *ChoreHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid chore id")
		return
	}
	existing, err := h.store.GetChore(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get chore")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "chore not found")
		return
	}

	var req createChoreRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	spokenBefore := tts.SpokenText(existing)
	if req.Title != "" {
		existing.Title = req.Title
	}
	if req.Description != "" {
		existing.Description = req.Description
	}
	if req.Category != "" {
		if req.Category != model.CategoryRequired && req.Category != model.CategoryCore && req.Category != model.CategoryBonus {
			writeError(w, http.StatusBadRequest, "category must be required, core, or bonus")
			return
		}
		existing.Category = req.Category
	}
	if req.Icon != "" {
		existing.Icon = req.Icon
	}
	// Honor an explicit 0 (nil == field omitted, non-nil == set to that
	// value). This lets admins clear a penalty or reset points to zero via
	// the UI rather than having the update silently dropped.
	if req.PointsValue != nil {
		if *req.PointsValue < 0 {
			writeError(w, http.StatusBadRequest, "points_value must be non-negative")
			return
		}
		existing.PointsValue = *req.PointsValue
	}
	if req.MissedPenaltyValue != nil {
		if *req.MissedPenaltyValue < 0 {
			writeError(w, http.StatusBadRequest, "missed_penalty_value must be non-negative")
			return
		}
		existing.MissedPenaltyValue = *req.MissedPenaltyValue
	}
	if req.EstimatedMinutes != nil {
		if *req.EstimatedMinutes < 0 {
			writeError(w, http.StatusBadRequest, "estimated_minutes must be non-negative")
			return
		}
		existing.EstimatedMinutes = req.EstimatedMinutes
	}
	// Always update booleans as they might be toggled off (or we could rely on a PATCH approach, but here we just assign)
	existing.RequiresApproval = req.RequiresApproval
	existing.RequiresPhoto = req.RequiresPhoto
	if req.PhotoSource != "" {
		if req.PhotoSource != model.PhotoSourceChild && req.PhotoSource != model.PhotoSourceExternal && req.PhotoSource != model.PhotoSourceBoth {
			writeError(w, http.StatusBadRequest, "photo_source must be child, external, or both")
			return
		}
		existing.PhotoSource = req.PhotoSource
	}

	if err := h.store.UpdateChore(r.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update chore")
		return
	}
	if audio := h.aiSvc.Audio(); audio != nil && (existing.TTSAudioURL == "" || tts.SpokenText(existing) != spokenBefore) {
		audio.GenerateAsync(*existing)
	}
	writeJSON(w, http.StatusOK, existing)
}

func (h *ChoreHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid chore id")
		return
	}
	if err := h.store.DeleteChore(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete chore")
		return
	}
	tts.Remove(id)
	w.WriteHeader(http.StatusNoContent)
}

// --- Schedules ---

type createScheduleRequest struct {
	AssignedTo         int64   `json:"assigned_to"`
	AssignmentType     string  `json:"assignment_type"`
	DayOfWeek          *int    `json:"day_of_week"`
	SpecificDate       *string `json:"specific_date"`
	AvailableAt        *string `json:"available_at"`
	DueBy              *string `json:"due_by"`
	ExpiryPenalty      string  `json:"expiry_penalty"`
	ExpiryPenaltyValue int     `json:"expiry_penalty_value"`
	PointsMultiplier   float64 `json:"points_multiplier"`
	StartDate          *string `json:"start_date"`
	EndDate            *string `json:"end_date"`
	RecurrenceInterval *int    `json:"recurrence_interval"`
	RecurrenceStart    *string `json:"recurrence_start"`
}

func scheduleFromRequest(choreID int64, req *createScheduleRequest) (*model.ChoreSchedule, string) {
	if req.AssignedTo == 0 {
		return nil, "assigned_to is required"
	}
	if req.RecurrenceInterval != nil {
		if *req.RecurrenceInterval < 1 {
			return nil, "recurrence_interval must be >= 1"
		}
		if req.RecurrenceStart == nil || *req.RecurrenceStart == "" {
			return nil, "recurrence_start is required with recurrence_interval"
		}
	} else if req.DayOfWeek == nil && req.SpecificDate == nil {
		return nil, "day_of_week, specific_date, or recurrence_interval is required"
	}
	if req.DayOfWeek != nil && (*req.DayOfWeek < 0 || *req.DayOfWeek > 6) {
		return nil, "day_of_week must be between 0 and 6"
	}
	if req.AssignmentType == "" {
		req.AssignmentType = "individual"
	}
	if req.PointsMultiplier == 0 {
		req.PointsMultiplier = 1.0
	}
	if req.PointsMultiplier < 0 {
		return nil, "points_multiplier must be positive"
	}
	if req.ExpiryPenalty == "" {
		req.ExpiryPenalty = model.ExpiryBlock
	}
	if req.ExpiryPenalty != model.ExpiryBlock && req.ExpiryPenalty != model.ExpiryNoPoints && req.ExpiryPenalty != model.ExpiryPenalty {
		return nil, "expiry_penalty must be block, no_points, or penalty"
	}
	if req.ExpiryPenalty == model.ExpiryPenalty && req.ExpiryPenaltyValue <= 0 {
		return nil, "expiry_penalty_value must be positive for penalty mode"
	}

	return &model.ChoreSchedule{
		ChoreID:            choreID,
		AssignedTo:         req.AssignedTo,
		AssignmentType:     req.AssignmentType,
		DayOfWeek:          req.DayOfWeek,
		SpecificDate:       req.SpecificDate,
		AvailableAt:        req.AvailableAt,
		DueBy:              req.DueBy,
		ExpiryPenalty:      req.ExpiryPenalty,
		ExpiryPenaltyValue: req.ExpiryPenaltyValue,
		PointsMultiplier:   req.PointsMultiplier,
		StartDate:          req.StartDate,
		EndDate:            req.EndDate,
		RecurrenceInterval: req.RecurrenceInterval,
		RecurrenceStart:    req.RecurrenceStart,
	}, ""
}

func (h *ChoreHandler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	choreID, err := urlParamInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid chore id")
		return
	}
	chore, err := h.store.GetChore(r.Context(), choreID)
	if err != nil || chore == nil {
		writeError(w, http.StatusNotFound, "chore not found")
		return
	}

	var req createScheduleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	schedule, validationError := scheduleFromRequest(choreID, &req)
	if validationError != "" {
		writeError(w, http.StatusBadRequest, validationError)
		return
	}
	if err := h.store.CreateSchedule(r.Context(), schedule); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create schedule")
		return
	}
	writeJSON(w, http.StatusCreated, schedule)
}

func (h *ChoreHandler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	choreID, err := urlParamInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid chore id")
		return
	}
	scheduleID, err := urlParamInt64(r, "scheduleID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid schedule id")
		return
	}
	existing, err := h.store.GetSchedule(r.Context(), scheduleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get schedule")
		return
	}
	if existing == nil || existing.ChoreID != choreID {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}
	var req createScheduleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	schedule, validationError := scheduleFromRequest(choreID, &req)
	if validationError != "" {
		writeError(w, http.StatusBadRequest, validationError)
		return
	}
	schedule.ID = scheduleID
	schedule.FcfsGroupID = existing.FcfsGroupID
	if err := h.store.UpdateSchedule(r.Context(), schedule); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update schedule")
		return
	}
	writeJSON(w, http.StatusOK, schedule)
}

func (h *ChoreHandler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	choreID, err := urlParamInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid chore id")
		return
	}
	schedules, err := h.store.ListSchedulesForChore(r.Context(), choreID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list schedules")
		return
	}
	if schedules == nil {
		schedules = []model.ChoreSchedule{}
	}
	writeJSON(w, http.StatusOK, schedules)
}

func (h *ChoreHandler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	choreID, err := urlParamInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid chore id")
		return
	}
	scheduleID, err := urlParamInt64(r, "scheduleID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid schedule id")
		return
	}
	schedule, err := h.store.GetSchedule(r.Context(), scheduleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get schedule")
		return
	}
	if schedule == nil || schedule.ChoreID != choreID {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}
	if err := h.store.DeleteSchedule(r.Context(), scheduleID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete schedule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Completions ---

type completeChoreRequest struct {
	CompletedBy    int64  `json:"completed_by"`
	CompletionDate string `json:"completion_date"`
	PhotoURL       string `json:"photo_url"`
	// SkipPhoto finishes a photo chore without one; it then waits for a
	// parent to approve instead.
	SkipPhoto bool `json:"skip_photo"`
}

// completionIsLate evaluates the deadline against the date the completion is
// for and the instant it was submitted. Comparing to minute precision keeps
// the UI contract: a 17:00 deadline remains open through 17:00:59. It also
// ensures back-dated submissions obey their original deadline.
func completionIsLate(schedule *model.ChoreSchedule, completionDate string, submittedAt time.Time) bool {
	if schedule == nil || schedule.DueBy == nil || *schedule.DueBy == "" {
		return false
	}
	return submittedAt.In(time.Local).Format(model.DateFormat+" 15:04") > completionDate+" "+*schedule.DueBy
}

// applyExpiryPolicy returns the points credit, late penalty debit, and whether
// completion is blocked. "Deduct points" means zero reward plus a debit; it is
// intentionally not a reduction from the chore's reward.
func applyExpiryPolicy(schedule *model.ChoreSchedule, completionDate string, submittedAt time.Time, points int) (int, int, bool) {
	if !completionIsLate(schedule, completionDate, submittedAt) {
		return points, 0, false
	}
	switch schedule.ExpiryPenalty {
	case model.ExpiryBlock:
		return 0, 0, true
	case model.ExpiryNoPoints:
		return 0, 0, false
	case model.ExpiryPenalty:
		return 0, schedule.ExpiryPenaltyValue, false
	default:
		return points, 0, false
	}
}

func (h *ChoreHandler) Complete(w http.ResponseWriter, r *http.Request) {
	scheduleID, err := urlParamInt64(r, "scheduleID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid schedule id")
		return
	}

	var req completeChoreRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.CompletionDate == "" {
		req.CompletionDate = time.Now().Format(model.DateFormat)
	}

	// Get the schedule to check time lock
	schedule, err := h.store.GetSchedule(r.Context(), scheduleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get schedule")
		return
	}
	if schedule == nil {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}
	if _, err := time.ParseInLocation(model.DateFormat, req.CompletionDate, time.Local); err != nil {
		writeError(w, http.StatusBadRequest, "completion_date must be YYYY-MM-DD")
		return
	}
	if req.CompletionDate > time.Now().Format(model.DateFormat) {
		writeError(w, http.StatusUnprocessableEntity, "future chores cannot be completed")
		return
	}
	occurs, err := h.store.ScheduleOccursOnDate(r.Context(), scheduleID, req.CompletionDate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate schedule date")
		return
	}
	if !occurs {
		writeError(w, http.StatusUnprocessableEntity, "this chore is not scheduled for the requested date")
		return
	}

	// Only the assignee (or a parent acting on their behalf) may complete.
	caller := UserFromContext(r.Context())
	if !canActOnSchedule(caller, schedule) {
		writeError(w, http.StatusForbidden, "this chore is assigned to someone else")
		return
	}
	completedBy, err := completerFor(caller, schedule, req.CompletedBy)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	actingForOther := caller.Role == model.RoleAdmin && completedBy != caller.ID

	// Enforce time lock
	now := time.Now()
	nowTime := now.Format("15:04")
	if schedule.AvailableAt != nil && *schedule.AvailableAt != "" {
		if nowTime < *schedule.AvailableAt {
			writeError(w, http.StatusUnprocessableEntity, "this chore isn't available until "+*schedule.AvailableAt)
			return
		}
	}

	// Check expiry against the requested chore date so a back-dated
	// completion cannot bypass the schedule's late policy.
	isExpired := completionIsLate(schedule, req.CompletionDate, now)

	// Enforce expiry penalty
	if isExpired && schedule.ExpiryPenalty == model.ExpiryBlock {
		writeError(w, http.StatusUnprocessableEntity, "this chore has expired and can no longer be completed")
		return
	}

	// FCFS race condition check: if a sibling already completed this FCFS group, reject
	if schedule.AssignmentType == model.AssignmentFCFS && schedule.FcfsGroupID != nil {
		done, err := h.store.FcfsGroupCompletedForDate(r.Context(), *schedule.FcfsGroupID, req.CompletionDate)
		if err == nil && done {
			writeError(w, http.StatusConflict, "a sibling already completed this chore")
			return
		}
	}

	user := UserFromContext(r.Context())

	// Check if already completed
	existing, err := h.store.GetCompletionForScheduleDate(r.Context(), scheduleID, req.CompletionDate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check completion")
		return
	}
	if existing != nil {
		if existing.UncompletedAt != nil {
			// Soft-deleted prior completion exists. Approved + pending rows
			// are revived in place so the kid keeps the photo / AI note /
			// approval metadata and doesn't have to retake a photo after an
			// accidental uncheck. Rejected rows are not revivable — treat
			// them as fresh retry targets by hard-deleting and falling
			// through to the normal complete flow.
			if existing.Status == model.StatusApproved || existing.Status == model.StatusPending {
				if err := h.store.ReviveCompletion(r.Context(), existing.ID); err != nil {
					writeError(w, http.StatusInternalServerError, "failed to revive completion")
					return
				}
				// A kid who skipped the photo may be re-checking with one now.
				if req.PhotoURL != "" && req.PhotoURL != existing.PhotoURL {
					if err := h.store.UpdateCompletionPhoto(r.Context(), existing.ID, req.PhotoURL); err != nil {
						writeError(w, http.StatusInternalServerError, "failed to attach photo")
						return
					}
					existing.PhotoURL = req.PhotoURL
					if existing.Status == model.StatusPending && (user == nil || user.Role != "admin") {
						h.queueReview(existing.ID)
					}
				}
				if user != nil && user.Role == "admin" && existing.Status == model.StatusPending {
					existing.Status = model.StatusApproved
					existing.ApprovedBy = &user.ID
					now := time.Now()
					existing.ApprovedAt = &now
					_ = h.store.UpdateCompletionStatus(r.Context(), existing.ID, model.StatusApproved, user.ID)
				}
				if existing.Status == model.StatusApproved {
					// Re-checking is a new points event, while the preserved completion
					// keeps its photo/review metadata. The previous completion and
					// uncomplete ledger rows remain as an audit trail; restore only the
					// points allowed by the gates and deadline right now.
					reviveChore, _ := h.store.GetChore(r.Context(), schedule.ChoreID)
					targetPoints, _ := h.store.GetChorePointsForSchedule(r.Context(), scheduleID)
					if reviveChore != nil && reviveChore.Category == model.CategoryCore && !h.shouldAwardCorePoints(r.Context(), existing.CompletedBy, req.CompletionDate) {
						targetPoints = 0
					}
					if reviveChore != nil && reviveChore.Category == model.CategoryBonus && !h.shouldAwardBonusPoints(r.Context(), existing.CompletedBy, req.CompletionDate) {
						targetPoints = 0
					}
					targetPoints, penalty, _ := applyExpiryPolicy(schedule, existing.CompletionDate, now, targetPoints)
					if targetPoints > 0 {
						if err := h.store.CreditChorePoints(r.Context(), existing.CompletedBy, existing.ID, targetPoints); err != nil {
							log.Printf("error restoring points on revive for completion %d: %v", existing.ID, err)
						}
					}
					if penalty > 0 {
						if err := h.store.DebitExpiryPenalty(r.Context(), existing.CompletedBy, existing.ID, penalty); err != nil {
							log.Printf("error restoring late penalty on revive for completion %d: %v", existing.ID, err)
						}
					}
					if reviveChore != nil && reviveChore.Category == model.CategoryRequired {
						h.creditPendingCorePoints(r.Context(), existing.CompletedBy, req.CompletionDate)
					}
					if reviveChore != nil && (reviveChore.Category == model.CategoryRequired || reviveChore.Category == model.CategoryCore) {
						h.creditPendingBonusPoints(r.Context(), existing.CompletedBy, req.CompletionDate)
					}
					// Recalculate streak after revival
					if err := h.store.RecalculateStreak(r.Context(), existing.CompletedBy, req.CompletionDate); err != nil {
						log.Printf("error recalculating streak for user %d: %v", existing.CompletedBy, err)
					}
				}
				// Note: we intentionally do NOT fire EventChoreCompleted on
				// revive. A revive isn't a fresh completion — it's reversing an
				// accidental uncheck — and firing would spam downstream webhook
				// consumers (Home Assistant scripts, push notifications) with
				// duplicate events. Revisit if product needs differ.
				// Clear uncompleted_at on the returned payload too
				existing.UncompletedAt = nil
				writeJSON(w, http.StatusCreated, existing)
				return
			}
			// Rejected soft-deleted: hard-delete the row so the retry flow
			// that follows can create a fresh completion.
			if err := h.store.UncompleteChore(r.Context(), scheduleID, req.CompletionDate); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to clear previous attempt")
				return
			}
		} else {
			writeError(w, http.StatusConflict, "chore already completed for this date")
			return
		}
	}

	// Fetch chore details to check category and requirements
	chore, _ := h.store.GetChore(r.Context(), schedule.ChoreID)

	// For "child" photo source the kid takes the photo at completion time.
	// For "external" or "both", a photo can be attached later.
	photoSource := model.PhotoSourceChild
	if chore != nil {
		photoSource = chore.PhotoSource
		if photoSource == "" {
			photoSource = model.PhotoSourceChild
		}
	}
	// A parent marking a chore done on someone's behalf vouches for it and
	// doesn't need to supply the photo. Anyone else can skip the photo
	// explicitly, and the chore then waits for a parent.
	missingPhoto := chore != nil && chore.RequiresPhoto && req.PhotoURL == "" && photoSource == model.PhotoSourceChild && !actingForOther
	if missingPhoto && !req.SkipPhoto {
		writeError(w, http.StatusBadRequest, "a photo is required to complete this chore")
		return
	}

	status := model.StatusApproved
	if chore != nil && (chore.RequiresApproval || missingPhoto) && (user == nil || user.Role != "admin") {
		status = model.StatusPending
	}

	var approvedBy *int64
	var approvedAt *time.Time
	if status == model.StatusApproved && user != nil && user.Role == "admin" {
		approvedBy = &user.ID
		now := time.Now()
		approvedAt = &now
	}

	completion := &model.ChoreCompletion{
		ChoreScheduleID: scheduleID,
		CompletedBy:     completedBy,
		Status:          status,
		PhotoURL:        req.PhotoURL,
		CompletionDate:  req.CompletionDate,
		ApprovedBy:      approvedBy,
		ApprovedAt:      approvedAt,
	}
	var pts int
	var expiryPenalty int
	// Only calculate points and streak if immediately approved
	if status == model.StatusApproved {
		// Credit or penalize points based on expiry status
		pts, _ = h.store.GetChorePointsForSchedule(r.Context(), scheduleID)

		// Core chore points only count once required chores are complete
		if chore != nil && chore.Category == model.CategoryCore {
			if !h.shouldAwardCorePoints(r.Context(), completedBy, req.CompletionDate) {
				pts = 0
			}
		}

		// Bonus chore points only count once required + core chores are complete
		if chore != nil && chore.Category == model.CategoryBonus {
			if !h.shouldAwardBonusPoints(r.Context(), completedBy, req.CompletionDate) {
				pts = 0
			}
		}

		pts, expiryPenalty, _ = applyExpiryPolicy(schedule, req.CompletionDate, now, pts)
	}

	if err := h.store.CompleteChoreAndCreditPoints(r.Context(), completion, pts, expiryPenalty); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete chore")
		return
	}

	if status == model.StatusPending && completion.PhotoURL != "" {
		h.queueReview(completion.ID)
	}

	if status == model.StatusApproved {
		// Completing a required chore can be the event that opens the core gate.
		if chore != nil && chore.Category == model.CategoryRequired {
			h.creditPendingCorePoints(r.Context(), completedBy, req.CompletionDate)
		}

		// Completing a required/core chore can be the event that opens the
		// bonus gate. Retroactively credit any approved bonus completions for
		// this user/date that were originally capped at 0.
		if chore != nil && (chore.Category == model.CategoryRequired || chore.Category == model.CategoryCore) {
			h.creditPendingBonusPoints(r.Context(), completedBy, req.CompletionDate)
		}

		// Recalculate streak
		if err := h.store.RecalculateStreak(r.Context(), completedBy, req.CompletionDate); err != nil {
			log.Printf("error recalculating streak for user %d: %v", completedBy, err)
		}
	}

	// FCFS: complete sibling schedules with shadow completions
	if schedule.AssignmentType == model.AssignmentFCFS && schedule.FcfsGroupID != nil && status == model.StatusApproved {
		if err := h.store.CompleteFCFSSiblings(r.Context(), *schedule.FcfsGroupID, completedBy, scheduleID, req.CompletionDate); err != nil {
			log.Printf("error completing FCFS siblings: %v", err)
		}

		// Fire FCFS-specific webhook
		completedByUser, _ := h.store.GetUser(r.Context(), completedBy)
		fcfsName := ""
		if completedByUser != nil {
			fcfsName = completedByUser.Name
		}
		fcfsTitle := ""
		if chore != nil {
			fcfsTitle = chore.Title
		}
		h.dispatcher.Fire(webhook.EventChoreFCFSCompleted, map[string]any{
			"completion_id":   completion.ID,
			"schedule_id":     scheduleID,
			"fcfs_group_id":   *schedule.FcfsGroupID,
			"chore_title":     fcfsTitle,
			"user_id":         completedBy,
			"user_name":       fcfsName,
			"completion_date": req.CompletionDate,
			"points_earned":   pts,
		})
	}

	// Fire webhook
	choreTitle := ""
	if chore != nil {
		choreTitle = chore.Title
	}
	completedByUser, _ := h.store.GetUser(r.Context(), completedBy)
	completedByName := ""
	if completedByUser != nil {
		completedByName = completedByUser.Name
	}

	// Determine absolute photo URL for webhooks
	absolutePhotoURL := req.PhotoURL
	if req.PhotoURL != "" {
		baseURL, _ := h.store.GetSetting(r.Context(), "base_url")
		if baseURL != "" {
			absolutePhotoURL = baseURL + req.PhotoURL
		}
	}

	h.dispatcher.Fire(webhook.EventChoreCompleted, map[string]any{
		"completion_id":   completion.ID,
		"schedule_id":     scheduleID,
		"chore_title":     choreTitle,
		"user_id":         completedBy,
		"user_name":       completedByName,
		"completion_date": req.CompletionDate,
		"points_earned":   pts,
		"status":          status,
		"photo_url":       absolutePhotoURL,
		"photo_source":    photoSource,
	})

	// Discord notification (non-blocking)
	if status == model.StatusPending {
		h.discord.NotifyPendingApproval(completedByName, choreTitle, absolutePhotoURL)
	} else {
		h.discord.NotifyCompleted(completedByName, choreTitle, absolutePhotoURL, pts)
	}

	// Check if all chores for today are done (only if this one was approved)
	if status == model.StatusApproved {
		go func() {
			todayChores, err := h.store.GetScheduledChoresForUser(context.Background(), completedBy, []string{req.CompletionDate}, time.Now())
			if err == nil {
				allDone := len(todayChores) > 0
				for _, c := range todayChores {
					if !c.Completed && c.Category != model.CategoryBonus {
						allDone = false
						break
					}
				}
				if allDone {
					h.dispatcher.Fire(webhook.EventDailyComplete, map[string]any{
						"user_id":   completedBy,
						"user_name": completedByName,
						"date":      req.CompletionDate,
					})
				}
			}
		}()
	}

	writeJSON(w, http.StatusCreated, completion)
}

func (h *ChoreHandler) Uncomplete(w http.ResponseWriter, r *http.Request) {
	scheduleID, err := urlParamInt64(r, "scheduleID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid schedule id")
		return
	}
	dateStr := r.URL.Query().Get("date")
	if dateStr == "" {
		dateStr = time.Now().Format(model.DateFormat)
	}

	// Get the schedule to check for FCFS
	schedule, _ := h.store.GetSchedule(r.Context(), scheduleID)
	if schedule != nil && !canActOnSchedule(UserFromContext(r.Context()), schedule) {
		writeError(w, http.StatusForbidden, "this chore is assigned to someone else")
		return
	}

	// Get completion before deleting so we can reverse points
	existing, _ := h.store.GetCompletionForScheduleDate(r.Context(), scheduleID, dateStr)
	// If the completion is already soft-deleted, short-circuit: don't debit
	// again (GetNetPointsForCompletion ignores chore_uncomplete rows, so the
	// already-debited amount would be re-debited), and don't call the store's
	// UncompleteChore (its fallback DELETE would destroy the preserved row).
	// The endpoint is idempotent.
	if existing != nil && existing.UncompletedAt != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var completedBy int64
	var completionID int64
	var netPoints int
	if existing != nil {
		completedBy = existing.CompletedBy
		completionID = existing.ID
		net, err := h.store.GetNetPointsForCompletion(r.Context(), existing.ID)
		if err == nil {
			netPoints = net
		}
	}

	// FCFS: uncomplete all siblings in the group
	if schedule != nil && schedule.AssignmentType == model.AssignmentFCFS && schedule.FcfsGroupID != nil {
		if err := h.store.UncompleteFCFSGroupAndDebitPoints(r.Context(), *schedule.FcfsGroupID, dateStr, completedBy, existing, netPoints); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to uncomplete FCFS group")
			return
		}
	} else {
		if err := h.store.UncompleteChoreAndDebitPoints(r.Context(), scheduleID, dateStr, completedBy, completionID, netPoints); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to uncomplete chore")
			return
		}
	}

	// Recalculate streak
	if completedBy > 0 {
		if err := h.store.RecalculateStreak(r.Context(), completedBy, dateStr); err != nil {
			log.Printf("error recalculating streak for user %d: %v", completedBy, err)
		}
	}

	// Fire webhook
	choreTitle := ""
	if schedule != nil {
		chore, _ := h.store.GetChore(r.Context(), schedule.ChoreID)
		if chore != nil {
			choreTitle = chore.Title
		}
	}
	uncompleteUser, _ := h.store.GetUser(r.Context(), completedBy)
	uncompleteUserName := ""
	if uncompleteUser != nil {
		uncompleteUserName = uncompleteUser.Name
	}
	h.dispatcher.Fire(webhook.EventChoreUncompleted, map[string]any{
		"schedule_id": scheduleID,
		"chore_title": choreTitle,
		"user_id":     completedBy,
		"user_name":   uncompleteUserName,
		"date":        dateStr,
	})

	w.WriteHeader(http.StatusNoContent)
}

// --- Approvals ---

func (h *ChoreHandler) ListPending(w http.ResponseWriter, r *http.Request) {
	pending, err := h.store.ListPendingCompletions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending completions")
		return
	}
	if pending == nil {
		pending = []store.PendingCompletionRow{}
	}
	writeJSON(w, http.StatusOK, pending)
}

func (h *ChoreHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid completion id")
		return
	}

	completion, err := h.store.GetCompletion(r.Context(), id)
	if err != nil || completion == nil {
		writeError(w, http.StatusNotFound, "completion not found")
		return
	}

	if completion.Status != model.StatusPending {
		writeError(w, http.StatusBadRequest, "completion is not pending")
		return
	}

	admin := UserFromContext(r.Context())
	if err := h.approveCompletion(r.Context(), completion, &admin.ID); err != nil {
		if errors.Is(err, store.ErrNotPending) {
			writeError(w, http.StatusBadRequest, "completion is not pending")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to approve")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// approveCompletion approves a pending completion and awards its points,
// then re-opens any core/bonus gates it unlocks and updates the streak.
// approverID is nil for an automatic (AI) approval. Returns
// store.ErrNotPending if someone else approved or rejected it first.
func (h *ChoreHandler) approveCompletion(ctx context.Context, completion *model.ChoreCompletion, approverID *int64) error {
	schedule, _ := h.store.GetSchedule(ctx, completion.ChoreScheduleID)
	var chore *model.Chore
	var pts int
	var expiryPenalty int
	if schedule != nil {
		pts, _ = h.store.GetChorePointsForSchedule(ctx, schedule.ID)
		chore, _ = h.store.GetChore(ctx, schedule.ChoreID)

		// Core points only count once required chores are done; bonus
		// points once required and core are done.
		if chore != nil && chore.Category == model.CategoryCore &&
			!h.shouldAwardCorePoints(ctx, completion.CompletedBy, completion.CompletionDate) {
			pts = 0
		}
		if chore != nil && chore.Category == model.CategoryBonus &&
			!h.shouldAwardBonusPoints(ctx, completion.CompletedBy, completion.CompletionDate) {
			pts = 0
		}

		// Apply the deadline using the original submission time. Approval can
		// happen hours later and must not change whether the work was late.
		pts, expiryPenalty, _ = applyExpiryPolicy(schedule, completion.CompletionDate, completion.CompletedAt, pts)
	}

	if err := h.store.ApproveCompletionAndCreditPoints(ctx, completion.ID, approverID, pts, expiryPenalty); err != nil {
		return err
	}

	// Approving a required completion can open the core gate, and a
	// required/core one the bonus gate: credit anything capped at 0.
	if chore != nil && chore.Category == model.CategoryRequired {
		h.creditPendingCorePoints(ctx, completion.CompletedBy, completion.CompletionDate)
	}
	if chore != nil && (chore.Category == model.CategoryRequired || chore.Category == model.CategoryCore) {
		h.creditPendingBonusPoints(ctx, completion.CompletedBy, completion.CompletionDate)
	}

	if err := h.store.RecalculateStreak(ctx, completion.CompletedBy, completion.CompletionDate); err != nil {
		log.Printf("error recalculating streak for user %d: %v", completion.CompletedBy, err)
	}

	userName := ""
	if u, _ := h.store.GetUser(ctx, completion.CompletedBy); u != nil {
		userName = u.Name
	}
	choreTitle := ""
	if chore != nil {
		choreTitle = chore.Title
	}
	h.discord.NotifyApproved(userName, choreTitle)
	return nil
}

func (h *ChoreHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid completion id")
		return
	}

	completion, err := h.store.GetCompletion(r.Context(), id)
	if err != nil || completion == nil {
		writeError(w, http.StatusNotFound, "completion not found")
		return
	}

	if completion.Status != model.StatusPending {
		writeError(w, http.StatusBadRequest, "completion is not pending")
		return
	}

	admin := UserFromContext(r.Context())
	if err := h.store.UpdateCompletionStatus(r.Context(), id, model.StatusRejected, admin.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reject")
		return
	}

	// Discord notification for rejection
	{
		userName := ""
		if u, _ := h.store.GetUser(r.Context(), completion.CompletedBy); u != nil {
			userName = u.Name
		}
		choreTitle := ""
		if schedule, _ := h.store.GetSchedule(r.Context(), completion.ChoreScheduleID); schedule != nil {
			if c, _ := h.store.GetChore(r.Context(), schedule.ChoreID); c != nil {
				choreTitle = c.Title
			}
		}
		h.discord.NotifyRejected(userName, choreTitle)
	}

	w.WriteHeader(http.StatusNoContent)
}

// AttachPhoto allows attaching or replacing a photo on a pending completion.
// This is used by external systems (e.g. Home Assistant) to provide photo proof
// after a chore has been marked complete.
func (h *ChoreHandler) AttachPhoto(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid completion id")
		return
	}

	completion, err := h.store.GetCompletion(r.Context(), id)
	if err != nil || completion == nil {
		writeError(w, http.StatusNotFound, "completion not found")
		return
	}

	if completion.Status != model.StatusPending {
		writeError(w, http.StatusBadRequest, "completion is not pending")
		return
	}

	var req struct {
		PhotoURL string `json:"photo_url"`
	}
	if err := decodeJSON(r, &req); err != nil || req.PhotoURL == "" {
		writeError(w, http.StatusBadRequest, "photo_url is required")
		return
	}

	if err := h.store.UpdateCompletionPhoto(r.Context(), id, req.PhotoURL); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to attach photo")
		return
	}
	h.queueReview(id)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":        id,
		"photo_url": req.PhotoURL,
	})
}

// shouldAwardBonusPoints returns true if all required and core chores for the
// given user and date are approved, meaning bonus points should be awarded.
func (h *ChoreHandler) shouldAwardBonusPoints(ctx context.Context, userID int64, date string) bool {
	todayChores, err := h.store.GetScheduledChoresForUser(ctx, userID, []string{date}, time.Now())
	if err != nil {
		return false
	}
	for _, c := range todayChores {
		if c.Category == model.CategoryRequired || c.Category == model.CategoryCore {
			if !c.Completed || c.CompletionStatus == nil || (*c.CompletionStatus != model.StatusApproved && *c.CompletionStatus != model.StatusExcused) {
				return false
			}
		}
	}
	return true
}

// shouldAwardCorePoints returns true if all required chores for the
// given user and date are approved or excused, meaning core points should be awarded.
func (h *ChoreHandler) shouldAwardCorePoints(ctx context.Context, userID int64, date string) bool {
	todayChores, err := h.store.GetScheduledChoresForUser(ctx, userID, []string{date}, time.Now())
	if err != nil {
		return false
	}
	for _, c := range todayChores {
		if c.Category == model.CategoryRequired {
			if !c.Completed || c.CompletionStatus == nil || (*c.CompletionStatus != model.StatusApproved && *c.CompletionStatus != model.StatusExcused) {
				return false
			}
		}
	}
	return true
}

// creditPendingBonusPoints retroactively credits approved bonus completions
// for the given user/date that were capped at 0 points because the
// required/core gate was closed at the time of their approval. Call after an
// event that can open the gate (a required or core chore transitioning to
// approved). No-op if the gate is still closed. Only the delta between the
// chore's full value and what's already on the completion is credited, so
// repeated calls can't multi-credit the same completion.
func (h *ChoreHandler) creditPendingBonusPoints(ctx context.Context, userID int64, date string) {
	if !h.shouldAwardBonusPoints(ctx, userID, date) {
		return
	}
	scheduled, err := h.store.GetScheduledChoresForUser(ctx, userID, []string{date}, time.Now())
	if err != nil {
		log.Printf("error fetching scheduled chores for bonus reevaluation user %d date %s: %v", userID, date, err)
		return
	}
	for _, sc := range scheduled {
		if sc.Category != model.CategoryBonus || sc.CompletionID == nil {
			continue
		}
		// Only approved completions get points; pending bonus completions
		// are credited when the admin approves them.
		if sc.CompletionStatus == nil || *sc.CompletionStatus != model.StatusApproved {
			continue
		}
		fullPts, err := h.store.GetChorePointsForSchedule(ctx, sc.ScheduleID)
		if err != nil {
			log.Printf("error fetching points for schedule %d: %v", sc.ScheduleID, err)
			continue
		}
		completion, err := h.store.GetCompletion(ctx, *sc.CompletionID)
		if err != nil || completion == nil {
			log.Printf("error fetching completion %d for bonus reevaluation: %v", *sc.CompletionID, err)
			continue
		}
		schedule, err := h.store.GetSchedule(ctx, sc.ScheduleID)
		if err != nil || schedule == nil {
			log.Printf("error fetching schedule %d for bonus reevaluation: %v", sc.ScheduleID, err)
			continue
		}
		fullPts, penalty, _ := applyExpiryPolicy(schedule, completion.CompletionDate, completion.CompletedAt, fullPts)
		targetNet := fullPts - penalty
		alreadyCredited, err := h.store.GetNetPointsForCompletion(ctx, *sc.CompletionID)
		if err != nil {
			log.Printf("error fetching net points for completion %d: %v", *sc.CompletionID, err)
			continue
		}
		delta := targetNet - alreadyCredited
		if delta > 0 {
			if err := h.store.CreditChorePoints(ctx, userID, *sc.CompletionID, delta); err != nil {
				log.Printf("error crediting retroactive bonus points for user %d completion %d: %v", userID, *sc.CompletionID, err)
			}
		}
	}
}

// creditPendingCorePoints retroactively credits approved core completions
// for the given user/date that were capped at 0 points because the
// required gate was closed at the time of their approval. Call after an
// event that can open the gate (a required chore transitioning to approved).
// No-op if the gate is still closed. Only the delta between the chore's
// full value and what's already on the completion is credited.
func (h *ChoreHandler) creditPendingCorePoints(ctx context.Context, userID int64, date string) {
	if !h.shouldAwardCorePoints(ctx, userID, date) {
		return
	}
	scheduled, err := h.store.GetScheduledChoresForUser(ctx, userID, []string{date}, time.Now())
	if err != nil {
		log.Printf("error fetching scheduled chores for core reevaluation user %d date %s: %v", userID, date, err)
		return
	}
	for _, sc := range scheduled {
		if sc.Category != model.CategoryCore || sc.CompletionID == nil {
			continue
		}
		// Only approved completions get points; pending completions
		// are credited when the admin approves them.
		if sc.CompletionStatus == nil || *sc.CompletionStatus != model.StatusApproved {
			continue
		}
		fullPts, err := h.store.GetChorePointsForSchedule(ctx, sc.ScheduleID)
		if err != nil {
			log.Printf("error fetching points for schedule %d: %v", sc.ScheduleID, err)
			continue
		}
		completion, err := h.store.GetCompletion(ctx, *sc.CompletionID)
		if err != nil || completion == nil {
			log.Printf("error fetching completion %d for core reevaluation: %v", *sc.CompletionID, err)
			continue
		}
		schedule, err := h.store.GetSchedule(ctx, sc.ScheduleID)
		if err != nil || schedule == nil {
			log.Printf("error fetching schedule %d for core reevaluation: %v", sc.ScheduleID, err)
			continue
		}
		fullPts, penalty, _ := applyExpiryPolicy(schedule, completion.CompletionDate, completion.CompletedAt, fullPts)
		targetNet := fullPts - penalty
		alreadyCredited, err := h.store.GetNetPointsForCompletion(ctx, *sc.CompletionID)
		if err != nil {
			log.Printf("error fetching net points for completion %d: %v", *sc.CompletionID, err)
			continue
		}
		delta := targetNet - alreadyCredited
		if delta > 0 {
			if err := h.store.CreditChorePoints(ctx, userID, *sc.CompletionID, delta); err != nil {
				log.Printf("error crediting retroactive core points for user %d completion %d: %v", userID, *sc.CompletionID, err)
			}
		}
	}
}

type ExcuseRequest struct {
	Date   string `json:"date"`
	Reason string `json:"reason"`
}

func (h *ChoreHandler) Excuse(w http.ResponseWriter, r *http.Request) {
	scheduleID, err := urlParamInt64(r, "scheduleID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid schedule id")
		return
	}

	var req ExcuseRequest
	_ = decodeJSON(r, &req)

	if req.Date == "" {
		req.Date = time.Now().Format(model.DateFormat)
	}

	admin := UserFromContext(r.Context())
	if admin == nil || admin.Role != "admin" {
		writeError(w, http.StatusForbidden, "admin access required to excuse chores")
		return
	}

	completion, err := h.store.ExcuseChoreAndRefundPenalty(r.Context(), scheduleID, req.Date, admin.ID, req.Reason)
	if err != nil {
		log.Printf("error excusing chore schedule %d: %v", scheduleID, err)
		writeError(w, http.StatusInternalServerError, "failed to excuse chore")
		return
	}

	// Excusing a required chore can open the core or bonus gate
	schedule, _ := h.store.GetSchedule(r.Context(), scheduleID)
	if schedule != nil {
		chore, _ := h.store.GetChore(r.Context(), schedule.ChoreID)
		if chore != nil && chore.Category == model.CategoryRequired {
			h.creditPendingCorePoints(r.Context(), schedule.AssignedTo, req.Date)
		}
		if chore != nil && (chore.Category == model.CategoryRequired || chore.Category == model.CategoryCore) {
			h.creditPendingBonusPoints(r.Context(), schedule.AssignedTo, req.Date)
		}
	}

	h.dispatcher.Fire("chore.excused", map[string]any{
		"schedule_id":     scheduleID,
		"completion_id":   completion.ID,
		"completion_date": req.Date,
		"excused_by":      admin.ID,
		"reason":          req.Reason,
	})

	writeJSON(w, http.StatusCreated, completion)
}

// completerFor decides whose completion this is (and so who is credited).
// Points always belong to the schedule's assignee, including when a parent
// ticks the chore on that person's behalf. The request field is accepted for
// compatibility but cannot redirect points to another profile.
func completerFor(caller *model.User, schedule *model.ChoreSchedule, requested int64) (int64, error) {
	if requested == 0 || requested == schedule.AssignedTo {
		return schedule.AssignedTo, nil
	}
	return 0, errors.New("completed_by must match the schedule assignee")
}
