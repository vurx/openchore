# OpenChore 积分处理逻辑

本文档汇总 OpenChore 当前所有主要积分处理逻辑，包括完成、审批、Multiplier、类别门槛、逾期、漏做、撤销、家长代办、Excuse、FCFS、Streak、奖励、储蓄目标、积分衰减与手动调整。

## 目录

1. [名词和计算公式](#1-名词和计算公式)
2. [积分系统总图](#2-积分系统总图)
3. [完成 Chore 的完整流程](#3-完成-chore-的完整流程)
4. [Required、Core、Bonus 门槛](#4-requiredcorebonus-门槛)
5. [Due By、迟到与漏做 Penalty](#5-due-by迟到与漏做-penalty)
6. [定时漏做检查](#6-定时漏做检查)
7. [重复点击、撤销和重新完成](#7-重复点击撤销和重新完成)
8. [家长代为完成与 Excuse](#8-家长代为完成与-excuse)
9. [FCFS 多孩子抢单](#9-fcfs-多孩子抢单)
10. [Streak、奖励、目标和积分衰减](#10-streak奖励目标和积分衰减)
11. [10 分任务完整示例](#11-10-分任务完整示例)
12. [Point Log 类型索引](#12-point-log-类型索引)

## 1. 名词和计算公式

- `P`：Chore 基础积分，即 `points_value`。
- `M`：Schedule 积分倍数，即 `points_multiplier`，默认 `1`。
- `MP`：漏做扣分，即 Chore 的 `missed_penalty_value`。
- `LP`：迟到扣分，即 Schedule 的 `expiry_penalty_value`。
- 实际基础奖励：`截断取整(P × M)`。
- 当前余额：该用户所有 `point_transactions.amount` 的合计。

```mermaid
flowchart LR
    P["Chore 基础积分 P"] --> CALC["P × Schedule Multiplier M"]
    M["Schedule Multiplier M"] --> CALC
    CALC --> TRUNC["截断小数部分"]
    TRUNC --> BASE["实际基础奖励"]
    BASE --> RULES["审批、类别门槛、迟到规则"]
    RULES --> LOG["Point Log"]
    LOG --> BALANCE["当前余额"]
```

例如：

- `10 × 1 = 10`
- `10 × 1.5 = 15`
- `5 × 1.5 = 7.5`，当前数据库转换结果为 `7`

Multiplier 只影响完成奖励，不放大漏做扣分或迟到扣分。

## 2. 积分系统总图

```mermaid
flowchart LR
    subgraph 增加积分
        A1["完成 Chore<br/>chore_complete"]
        A2["连续完成奖励<br/>streak_bonus"]
        A3["家长手动增加<br/>admin_adjust"]
        A4["取消目标或目标退款<br/>goal_break"]
        A5["Excuse 退回漏做扣分<br/>admin_adjust"]
    end

    subgraph 减少积分
        B1["撤销完成<br/>chore_uncomplete"]
        B2["迟到扣分<br/>expiry_penalty"]
        B3["漏做扣分<br/>missed_chore"]
        B4["积分衰减<br/>points_decay"]
        B5["兑换奖励<br/>reward_redeem"]
        B6["存入目标<br/>commit_to_goal"]
        B7["家长手动扣分<br/>admin_adjust"]
    end

    A1 --> LEDGER
    A2 --> LEDGER
    A3 --> LEDGER
    A4 --> LEDGER
    A5 --> LEDGER
    B1 --> LEDGER
    B2 --> LEDGER
    B3 --> LEDGER
    B4 --> LEDGER
    B5 --> LEDGER
    B6 --> LEDGER
    B7 --> LEDGER

    LEDGER["Point Log<br/>point_transactions"] --> BALANCE["当前余额<br/>SUM amount"]
```

所有积分变化都通过 Point Log 表示，不直接维护一个可被任意覆盖的余额数字。

## 3. 完成 Chore 的完整流程

```mermaid
flowchart TD
    START["孩子或家长点击完成"] --> DATE{"日期是否合法？"}

    DATE -- "未来日期" --> R1["拒绝完成<br/>积分 0"]
    DATE -- "不是该 Schedule 的日期" --> R2["拒绝完成<br/>积分 0"]
    DATE -- "日期正确" --> ASSIGNEE{"操作人有权限吗？"}

    ASSIGNEE -- "其他无关孩子" --> R3["拒绝完成<br/>积分 0"]
    ASSIGNEE -- "被分配孩子或家长" --> AVAILABLE{"到 Available At 了吗？"}

    AVAILABLE -- "还没到" --> R4["拒绝完成<br/>积分 0"]
    AVAILABLE -- "已开放" --> DUPLICATE{"Schedule + 日期<br/>是否已有完成？"}

    DUPLICATE -- "已有有效完成" --> R5["拒绝重复完成<br/>不会重复加分"]
    DUPLICATE -- "以前撤销过" --> REVIVE["恢复原 Completion<br/>重新检查当前规则"]
    DUPLICATE -- "没有" --> REQUIREMENT{"是否需要审批？"}

    REQUIREMENT -- "孩子提交且需要审批" --> PENDING["Pending<br/>暂时积分 0"]
    REQUIREMENT -- "需要照片但孩子跳过照片" --> PENDING
    REQUIREMENT -- "家长代为完成" --> APPROVED["直接 Approved"]
    REQUIREMENT -- "不需要审批" --> APPROVED

    PENDING --> REVIEW{"家长或 AI 审批"}
    REVIEW -- "Reject" --> REJECTED["Rejected<br/>积分 0<br/>仍可能被判定漏做"]
    REVIEW -- "Approve" --> APPROVED

    APPROVED --> CALC["计算实际基础奖励<br/>截断取整 P × M"]
    CALC --> CATEGORY["检查 Required / Core / Bonus 门槛"]
    CATEGORY --> LATE["检查提交时是否超过 Due By"]
    LATE --> LEDGER["写入 Point Log"]
    LEDGER --> OWNER["积分归 Schedule Assigned To 的孩子"]

    REVIVE --> CALC
```

审批发生在几小时以后时，迟到判断使用原始 `completed_at`，不会使用家长批准时间。

## 4. Required、Core、Bonus 门槛

```mermaid
flowchart TD
    COMPLETE["Chore 已完成并 Approved"] --> TYPE{"Chore 类型"}

    TYPE -- "Required" --> REQUIRED["立即进入积分计算"]
    TYPE -- "Core" --> REQUIRED_GATE{"当天所有 Required<br/>是否 Approved 或 Excused？"}
    TYPE -- "Bonus" --> BONUS_GATE{"当天所有 Required + Core<br/>是否 Approved 或 Excused？"}

    REQUIRED_GATE -- "否" --> CORE_ZERO["Core 已完成<br/>暂时奖励 0"]
    REQUIRED_GATE -- "是" --> CORE_POINTS["发放 Core 积分"]

    BONUS_GATE -- "否" --> BONUS_ZERO["Bonus 已完成<br/>暂时奖励 0"]
    BONUS_GATE -- "是" --> BONUS_POINTS["发放 Bonus 积分"]

    REQUIRED --> OPEN_CORE["可能打开 Core 门槛"]
    OPEN_CORE --> RETRO_CORE["重新检查之前 Approved<br/>但记 0 分的 Core"]

    REQUIRED --> OPEN_BONUS["可能打开 Bonus 门槛"]
    CORE_POINTS --> OPEN_BONUS
    OPEN_BONUS --> RETRO_BONUS["重新检查之前 Approved<br/>但记 0 分的 Bonus"]

    RETRO_CORE --> DELTA1["只补差额<br/>不会重复发完整积分"]
    RETRO_BONUS --> DELTA2["只补差额<br/>不会重复发完整积分"]
```

```mermaid
flowchart LR
    BONUS["Bonus 先完成<br/>暂时 +0"] --> CORE["Core 完成<br/>Required 未完成<br/>暂时 +0"]
    CORE --> REQUIRED["Required 最后完成<br/>发放 Required 积分"]
    REQUIRED --> RETRO_CORE["补发 Core 积分"]
    RETRO_CORE --> RETRO_BONUS["补发 Bonus 积分"]
```

`Excused` 可以满足类别门槛，但 Excused 自己不会获得完成积分。

## 5. Due By、迟到与漏做 Penalty

假设：

- 基础积分 `P = 10`
- Multiplier `M = 1`
- 漏做 Penalty `MP = 5`
- 迟到扣分 `LP = 2`

```mermaid
flowchart TD
    CHECK["提交完成"] --> LATE{"提交时间是否晚于<br/>完成日期 + Due By？"}

    LATE -- "否，按时" --> NORMAL["chore_complete +10<br/>最终 +10"]
    LATE -- "是，迟到" --> POLICY{"If completed late"}

    POLICY -- "Block" --> BLOCK["禁止完成<br/>暂时积分 0"]
    POLICY -- "No points" --> ZERO["允许完成<br/>完成奖励 0<br/>最终 0"]
    POLICY -- "Deduct 2" --> DEDUCT["允许完成<br/>完成奖励 0<br/>expiry_penalty -2<br/>最终 -2"]

    BLOCK --> NEXTDAY{"之后仍然未完成？"}
    NEXTDAY -- "是" --> MISSED["定时检查可能再扣<br/>missed_chore -5"]
    NEXTDAY -- "家长 Excuse" --> EXCUSED["不扣漏做 Penalty"]

    ZERO --> DONE["任务算已完成<br/>不会再扣漏做 Penalty"]
    DEDUCT --> DONE
```

`Deduct points` 的定义：

```mermaid
flowchart LR
    WRONG["不是：10 - 2 = +8"] --> ACTUAL["实际：完成奖励 0，再扣 2"]
    ACTUAL --> RESULT["最终变化 -2"]
```

Multiplier 为 `1.5` 时：

```mermaid
flowchart TD
    BASE["基础分 10"] --> MULTI["10 × 1.5 = 15"]
    MULTI --> ONTIME["按时完成：+15"]
    MULTI --> LATE_ZERO["迟到 No points：0"]
    MULTI --> LATE_DEDUCT["迟到 Deduct 2：-2"]
    MULTI --> MISS["未完成：-5"]
```

## 6. 定时漏做检查

```mermaid
flowchart TD
    TIMER["定时检查任务运行"] --> DAY["检查昨天的 Schedule"]
    DAY --> PAUSED{"孩子是否 Paused？"}

    PAUSED -- "是" --> SKIP_PAUSED["跳过，不扣分"]
    PAUSED -- "否" --> CATEGORY{"Chore 类型"}

    CATEGORY -- "Bonus" --> SKIP_BONUS["Bonus 可选<br/>不扣漏做 Penalty"]
    CATEGORY -- "Required 或 Core" --> STATUS{"昨天的状态"}

    STATUS -- "Approved" --> NO1["已完成，不扣"]
    STATUS -- "Pending" --> NO2["已提交等待审批，不扣"]
    STATUS -- "Excused" --> NO3["家长已跳过，不扣"]
    STATUS -- "Rejected" --> PENALTY_CHECK["视为未完成"]
    STATUS -- "没有 Completion" --> PENALTY_CHECK

    PENALTY_CHECK --> VALUE{"Missed Penalty > 0？"}
    VALUE -- "否" --> NO4["不扣"]
    VALUE -- "是" --> IDEMPOTENT{"孩子 + Schedule + 日期<br/>是否已经扣过？"}

    IDEMPOTENT -- "已经扣过" --> NO5["不重复扣"]
    IDEMPOTENT -- "没有扣过" --> DEBIT["写入 missed_chore<br/>扣 Missed Penalty"]
```

同一个用户、Schedule、日期具有唯一幂等键。即使定时任务重复执行，也只会扣一次。

## 7. 重复点击、撤销和重新完成

```mermaid
flowchart TD
    FIRST["第一次完成"] --> LOG1["chore_complete +10"]
    LOG1 --> SECOND{"再次点击完成"}

    SECOND -- "Completion 仍有效" --> CONFLICT["返回已完成<br/>不新增积分"]
    SECOND -- "Completion 已撤销" --> REVIVE["恢复原 Completion"]

    LOG1 --> UNDO["撤销完成"]
    UNDO --> NET["计算该 Completion 当前净积分"]

    NET --> CASE1["普通完成：净积分 +10"]
    NET --> CASE2["迟到扣分：净积分 -2"]
    NET --> CASE3["Pending：净积分 0"]

    CASE1 --> REVERSE1["chore_uncomplete -10"]
    CASE2 --> REVERSE2["chore_uncomplete +2<br/>退回迟到扣分"]
    CASE3 --> REVERSE3["无需积分冲正"]

    REVERSE1 --> BALANCE0["该次完成净影响恢复为 0"]
    REVERSE2 --> BALANCE0
    REVERSE3 --> BALANCE0

    REVIVE --> RECHECK["重新检查当前类别门槛和截止时间"]
    RECHECK --> NEW_ON_TIME["满足规则：重新写奖励"]
    RECHECK --> NEW_LATE["已经迟到：按当前迟到策略处理"]
```

撤销接口是幂等的：连续撤销不会连续扣分。

```mermaid
flowchart LR
    COMPLETE["chore_complete +10"] --> UNCOMPLETE["chore_uncomplete -10"]
    UNCOMPLETE --> TOTAL["Point Log 合计 0"]
```

原日志不会被覆盖，撤销通过相反方向的新日志表达。

## 8. 家长代为完成与 Excuse

### 8.1 家长代为完成

```mermaid
flowchart LR
    PARENT["家长点击完成"] --> APPROVED["家长视为已经确认<br/>直接 Approved"]
    APPROVED --> OWNER["积分归 Schedule 的 Assigned To"]
    OWNER --> CHILD["孩子获得积分"]
```

家长不能通过伪造 `completed_by` 把积分转给其他孩子。

### 8.2 家长 Excuse

```mermaid
flowchart TD
    EXCUSE["家长选择 Excuse"] --> EXIST{"当前日期是否已有 Completion？"}

    EXIST -- "没有" --> CREATE["创建 Excused Completion"]
    EXIST -- "Pending 或 Rejected" --> UPDATE["改成 Excused"]
    EXIST -- "已经 Approved" --> REVERSE["计算 Completion 净积分并冲正"]

    REVERSE --> ORIGINAL{"原净积分"}
    ORIGINAL -- "+10" --> REMOVE_REWARD["chore_uncomplete -10"]
    ORIGINAL -- "-2 迟到扣分" --> REFUND_LATE["chore_uncomplete +2"]
    ORIGINAL -- "0" --> NO_CHANGE["无需积分冲正"]

    CREATE --> MISSED{"是否已经扣过 missed_chore？"}
    UPDATE --> MISSED
    REMOVE_REWARD --> MISSED
    REFUND_LATE --> MISSED
    NO_CHANGE --> MISSED

    MISSED -- "已经扣 -5" --> REFUND_MISSED["admin_adjust +5<br/>退回漏做 Penalty"]
    MISSED -- "没有扣过" --> GATE["无需退款"]

    REFUND_MISSED --> GATE
    GATE --> OPEN["Excused 可打开 Core / Bonus 门槛"]
    OPEN --> RETRO["可能触发 Core / Bonus 补发"]
```

```mermaid
flowchart LR
    EXCUSED["Excused"] --> A["自己不获得完成积分"]
    EXCUSED --> B["自己不承担漏做扣分"]
    EXCUSED --> C["已有完成积分会被冲正"]
    EXCUSED --> D["已有漏做或迟到扣分会被退回"]
    EXCUSED --> E["可帮助打开类别门槛"]
```

## 9. FCFS 多孩子抢单

```mermaid
flowchart TD
    GROUP["同一个 FCFS Group<br/>分配给多个孩子"] --> CLICK["第一个孩子完成"]

    CLICK --> WINNER["该孩子成为 Winner"]
    WINNER --> POINTS["积分只发给 Winner"]
    WINNER --> SHADOW["其他兄弟 Schedule<br/>创建 0 分 shadow completion"]

    SHADOW --> SECOND["另一个孩子再点击"]
    SECOND --> REJECT["Group 已完成<br/>拒绝重复领取"]

    POINTS --> UNDO["撤销任意一个 Group Schedule"]
    UNDO --> FIND["找到真正获得积分的 Completion"]
    FIND --> REVERSE["冲正 Winner 的净积分"]
    REVERSE --> CLEAR["整组 Completion 一起撤销"]
```

## 10. Streak、奖励、目标和积分衰减

### 10.1 Streak

```mermaid
flowchart TD
    S1["Approved 或 Excused<br/>参与连续完成判断"] --> S2["重新计算 Streak"]
    S2 --> S3{"达到里程碑？"}
    S3 -- "是" --> S4["streak_bonus +N"]
    S3 -- "否" --> S5["不加奖励"]
    S4 --> S6["同一段 Streak 的同一里程碑<br/>只奖励一次"]

    S6 --> LATER["后来撤销某个 Chore"]
    LATER --> RECALC["Streak 重新计算"]
    RECALC --> KEEP["当前不会自动冲正<br/>已经发放的 streak_bonus"]
```

### 10.2 奖励兑换

```mermaid
flowchart TD
    R1["兑换 Reward"] --> R2{"余额和库存足够？"}
    R2 -- "否" --> R3["拒绝兑换"]
    R2 -- "是" --> R4["reward_redeem -Cost"]
```

### 10.3 储蓄目标

```mermaid
flowchart TD
    G1["存入 Goal"] --> G2["commit_to_goal -Amount"]
    G2 --> G3["可消费余额减少<br/>目标进度增加"]
    G3 --> G4{"取消 Goal？"}
    G4 -- "是" --> G5["goal_break +Refund"]
    G4 -- "否" --> G6["继续保留在目标中"]
```

### 10.4 积分衰减

```mermaid
flowchart TD
    D1["Decay 定时任务"] --> D2["计算本次应扣积分"]
    D2 --> D3["先使用可消费余额"]
    D3 --> D4{"可消费余额是否足够？"}
    D4 -- "是" --> D5["points_decay -Amount"]
    D4 -- "否" --> D6["先从个人 Goal 释放积分"]
    D6 --> D7["再释放自己的家庭 Goal 份额"]
    D7 --> D5
    D5 --> D8["最多扣到 0<br/>不会把余额扣成负数"]
```

目标释放顺序：个人目标优先，然后是自己的家庭共享目标份额；同类目标中优先释放较新的目标。

### 10.5 家长手动调整

```mermaid
flowchart TD
    A1["家长手动调整"] --> A2{"Amount 正负"}
    A2 -- "正数" --> A3["admin_adjust +N"]
    A2 -- "负数" --> A4["admin_adjust -N"]
```

## 11. 10 分任务完整示例

参数：

```text
Points = 10
Multiplier = 1
Missed Penalty = 5
Late Deduct = 2
```

```mermaid
flowchart TD
    TASK["10 分 Chore"] --> SCENE{"发生什么？"}

    SCENE -- "按时 Approved" --> A["+10"]
    SCENE -- "Pending" --> B["0<br/>批准后再计算"]
    SCENE -- "Rejected" --> C["0<br/>之后可能漏做 -5"]
    SCENE -- "迟到 Block" --> D["不能完成<br/>之后可能漏做 -5"]
    SCENE -- "迟到 No points" --> E["完成成功<br/>0"]
    SCENE -- "迟到 Deduct 2" --> F["完成成功<br/>-2"]
    SCENE -- "一直未完成" --> G["定时检查<br/>-5"]
    SCENE -- "家长代完成" --> H["孩子 +10"]
    SCENE -- "家长 Excuse" --> I["0<br/>已有奖励或扣分全部冲正"]
    SCENE -- "完成后撤销" --> J["+10 再 -10<br/>合计 0"]
    SCENE -- "反复点击完成" --> K["只记第一次<br/>+10"]
    SCENE -- "Multiplier 1.5" --> L["按时 +15<br/>迟到仍为 0 或 -2"]
```

完整计算顺序：

```mermaid
flowchart LR
    BASE["计算完成奖励<br/>截断取整 Points × Multiplier"]
    BASE --> APPROVAL["检查审批状态"]
    APPROVAL --> GATE["检查类别门槛"]
    GATE --> LATE["应用迟到规则"]
    LATE --> LOG["写入 Point Log"]
    LOG --> BALANCE["所有日志求和得到余额"]
```

## 12. Point Log 类型索引

| Reason | 方向 | 用途 |
|---|---:|---|
| `chore_complete` | 正数或 0 | Chore 完成奖励或补发奖励 |
| `chore_uncomplete` | 正数或负数 | 撤销完成、冲正奖励或退回迟到扣分 |
| `streak_bonus` | 正数 | Streak 里程碑奖励 |
| `admin_adjust` | 正数或负数 | 家长手动调整、Excuse 退回漏做扣分 |
| `expiry_penalty` | 负数 | 迟到完成扣分 |
| `missed_chore` | 负数 | 定时任务发现 Required/Core 漏做 |
| `points_decay` | 负数 | 周期性积分衰减 |
| `reward_redeem` | 负数 | 兑换奖励 |
| `commit_to_goal` | 负数 | 把可消费积分存入目标 |
| `goal_break` | 正数 | 取消目标、释放目标积分或衰减回收 |

