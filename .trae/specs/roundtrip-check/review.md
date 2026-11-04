# Review 记录 - yq 离线 Round-Trip 检查流程

## Review Cycle 1（实现后独立审查）

- **Reviewer**: 全新上下文的 general_purpose_task 子代理（只读，未修改文件）
- **范围**: spec.md FR-1..16 / NFR-1..5 / AC-1..AC-10；全部新增文件 + root.go/yq.go 接线 + 验收脚本
- **验证命令**: `go test ./pkg/yqlib/ ./cmd/ -run RoundTrip`、`go vet ./...`、`go build`（含最小标签集）、`acceptance_tests/roundtrip.sh`、约 20 个手工 CLI 场景、跨进程确定性 cmp
- **Result**: `fail`

### 通过项
- AC-1/3/4/6/7/8 全部 PASS 且证据充分；AC-9 4/5、AC-10 4/5（达通过线）
- 专项风险（全新 encoder/decoder 实例、跨文件 parse 隔离、别名 explode、bufio+Flush、原子写清理、多文件确定性、无 panic 面、全局状态无泄漏）均核实正确

### 可执行发现（已登记为 tasks.md 修复项）

| ID | 级别 | 位置 | 摘要 | 处置 |
|---|---|---|---|---|
| F1 | blocker | cmd/roundtrip_command.go | 未知 flag 在 RunE 前由 cobra 返回普通错误，退出码为 1 而非 NFR-1 要求的 2，且打印 Usage | 修复：SetFlagErrorFunc 包 ExitCodeError(2)，命令级 SilenceUsage |
| F2 | major | pkg/yqlib/roundtrip_flow.go | `--document` 筛选后 finding 文档序号被重编号（选 0,2 时源 D2 被标 D1）；报告头 documents 计数（全部解码数）与 doc_count finding（入选数）口径不一致 | 修复：选择结果携带原始文档序号，所有 finding 使用原序号；头部计数改为实际受检文档数 |
| F3 | minor | pkg/yqlib/roundtrip_compare.go | 等长但键集合不同（有 missing/added）时误报 key_order_changed | 修复：双向集合相等才报键序 |
| F4 | minor | 测试 | phase=redecode 失败分支无测试；CLI 层缺纯键序两种策略退出码测试；tasks 证据计数不准 | 修复：补 redecode 失败（同包伪 Format）、CLI 键序两策略用例；更正计数 |
| F5 | minor | roundtrip_report.go | summary 中状态计数与 finding 计数同名连排（parse_error/encode_error 重复出现） | 修复：拆成 files: 与 findings: 两段 |
| F6 | minor | roundtrip_flow.go | `--path` 用可写 Context 求值，赋值类表达式（`.a = 5`）会静默改写被检树 | 修复：ReadOnlyClone 求值 |
| F7 | advisory | roundtrip_flow.go | 路径表达式运行期错误被错分为 encode_error(encode) | 修复（随 F6）：作为操作错误返回（exit 2） |
| F8 | advisory | pkg/yqlib/doc/operators/system-operators.md | 与本任务无关的预存工作区改动 | 不改：非本次产生，保持原样 |
| F9 | advisory | roundtrip_flow.go | encode_error 时头部 `1 -> 0` 无 doc_count finding | 不改：数值真实，属可解释口径 |
| F10 | advisory | cmd/roundtrip_command.go | `--compare ""` 静默按默认、空 document token 被跳过 | 修复：显式报错 |
| F11 | advisory | cmd | 无参数且 tty stdin 时空检查报 ok | 不改：继承既有 stdin 语义 |
| F12 | advisory | roundtrip_compare.go | encoder 支持别名时 AliasNode 按叶子比较 | 不改：内置格式不可达差异场景 |

## Review Cycle 2（R1–R6 修复后独立复评）

- **Reviewer**: 全新上下文的 general_purpose_task 子代理（只读，未修改文件、未做 git 操作），独立运行测试与手工命令，未仅采信 tasks.md
- **Result**: `pass`（无新 blocker/major/minor）
- **逐条验证**:
  - R1 PASS：SilenceUsage/SilenceErrors + SetFlagErrorFunc；`--nope` 实测 exit 2、stderr 有 Error、无 Usage；CLI/验收测试覆盖。
  - R2 PASS：`selectedRoundTripDocument{node, originalIndex}` 贯穿全部 finding；流级错误与尾部 added 无序号；`--document 0,2` props 实测头部与 doc_count 均 `2 -> 1`、finding 标 D0/D2、无 D1。
  - R3 PASS：sameKeySet（等长且双向集合相等）前置；点号键 props 场景仅 missing/added/type_changed，无 key_order_changed。
  - R4 PASS：伪 Format（写不可解析 YAML 字节 + yaml decoder）覆盖 phase=redecode；XML 属性/元素重排 semantic=0（informational）/ordered=1，库测+CLI+验收三处覆盖。
  - R5 PASS：text summary 拆 files:/findings: 两段；黄金串更新。
  - R6 PASS：path 对 `node.Copy()` 深拷贝 + DontAutoCreate 求值，赋值测试断言原节点不变；`error("boom")`/`--compare ""`/`--document "1,"` 实测均 exit 2。
- **附加复核通过**: 接线零影响（root.go +1 行、yq.go errors.As 缺省仍 1、eval 冒烟）；原子写失败无残留；text/json 跨进程 cmp 逐字节一致、无颜色时间戳；parse/encode(encode|redecode)/semantic 分类在两种报告中均可机器区分；多文件 parse 隔离；FR-6 路径前缀正确；NFR-5 最小标签构建成功；gofmt/spelling 通过。
- **验证命令结果**: RoundTrip 相关 56 个测试（yqlib 36 + cmd 20）全 PASS；`go vet ./...` 通过；15 项 roundtrip 验收 + 全量 18 脚本 OK；最小标签构建成功；全量 `go test ./...` 仅基线 TestJSONScenarios（环境噪声，已声明）失败。
- **保留的非阻塞观察**: F9/F11/F12（Cycle 1 已接受的 advisory）；新增一条理论性观察——mappingKeyValueEntries 对重复键取最后值（spec 未涵盖重复键，两侧同函数处理，误报风险低），不入 issue。

## Review History
- Cycle 1: fail（F1 blocker/F2 major/F3-F6 minor/F7/F10 advisory 已修复，F8/F9/F11/F12 接受不改）。
- Cycle 2: pass（R1–R6 全部独立验证到位，无新问题）→ 功能交付完成。
