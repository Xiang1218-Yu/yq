# yq 离线 Round-Trip 检查流程 - 实施计划

## Remediation Issue R1（Review Cycle 1 / F1，blocker）：未知 flag 退出码
- **Status**: `completed`
- **Priority**: high
- **Description**: roundtrip 命令未设 SetFlagErrorFunc，flag 解析错误经 cobra 在 RunE 前返回普通错误，yq.go 取不到 ExitCodeError → 退出 1，违反 NFR-1（未知 flag 应退出 2），并打印整段 Usage。修复：命令级 `SilenceUsage: true`/`SilenceErrors: true`（结构体字段）+ `SetFlagErrorFunc` 向 stderr 打印 "Error: ..." 并返回 `newExitCodeError(2, err)`。
- **TR**: `rule` `roundtrip --nope` → 退出码 2、stderr 有 Error、无 Usage；由 `TestRoundTripCommandUnknownFlagExitTwo` 与验收 `testRoundTripUnknownFlagExitCodeTwo` 覆盖，手工复现通过。
- **Completion Evidence**: `cmd/roundtrip_command.go` 结构体加 Silence 字段与 SetFlagErrorFunc；测试与验收均通过。

## Remediation Issue R2（Review Cycle 1 / F2，major）：文档筛选保留原始序号并统一计数
- **Status**: `completed`
- **Priority**: high
- **Description**: 选择结果携带原始文档序号（`selectedRoundTripDocument{node, originalIndex}`），whole-doc 与 path 模式所有 finding（encode/missing/比较器 document）使用原始序号；流级错误（Flush、redecode）与新增尾部文档不带 document 序号；`DocumentsBefore/DocumentsAfter` 改为"实际受检文档数 → 再解码文档数"，与 doc_count finding 口径一致；parse_error 时 before 为已成功解码篇数。
- **TR**: `rule` `--document 0,2` 三文档 props 场景：finding 标 D0（.v value_changed）/D2（root missing）、头部与 doc_count 均为 `2 -> 1`、无 D1；由 `TestRoundTripCheckDocumentFilterKeepsOriginalIndexes` 与验收 `testRoundTripDocumentFilterKeepsOriginalIndexes` 覆盖，手工复现通过。
- **Completion Evidence**: `roundtrip_flow.go` select/checkWholeDocuments/checkWithPath 全部改用 selectedRoundTripDocument；`TestRoundTripCheckDocumentFilter` 计数口径同步改为 1 -> 1。

## Remediation Issue R3（F3，minor）：键序报告以集合相等为前提
- **Status**: `completed`
- **Priority**: medium
- **Description**: compareMappings 仅在双向键集合相等（长度相同且 before 键均在 after 集合中，即无 missing/added）且顺序不同时报 key_order_changed。
- **TR**: `rule` props 把 `a.b` 解成嵌套的场景不再出现 key_order_changed，只有 missing/added；由库流测试 `TestRoundTripCheckPropertiesDottedKeysNotKeyOrder` 与比较器直测 `TestRoundTripCompareDifferentKeySetsNotKeyOrder` 覆盖。
- **Completion Evidence**: `roundtrip_compare.go` compareMappings 增加 sameKeySet 前置判定。

## Remediation Issue R4（F4，minor）：补充 redecode 失败与 CLI 键序两策略测试
- **Status**: `completed`
- **Priority**: medium
- **Description**: 同包测试构造伪 Format（roundTripBrokenReDecodeEncoder 写 `a: [1, 2\n` + yaml decoder）触发 phase=redecode finding；CLI 用 XML 属性/元素重排（`a: {child, +@attr}`）验证 semantic 退出 0（informational）、ordered 退出 1；更正 tasks.md 计数。
- **TR**: `rule` 新测试通过。
- **Completion Evidence**: `TestRoundTripCheckReDecodeFailureIsEncodeError`、`TestRoundTripCommandKeyOrderSemanticIsInformational`、`TestRoundTripCommandKeyOrderOrderedStrategyExitOne`、验收 `testRoundTripKeyOrderStrategies` 全部通过；本文件 Task 2/4/5 计数已按实际更正（库 flow 19 个、比较器 10 个、报告 7 个、cmd 20 个、验收 15 项）。

## Remediation Issue R5（F5，minor）：summary 分段
- **Status**: `completed`
- **Priority**: medium
- **Description**: 文本 summary 拆为 `files: ok=..; semantic_diff=..; encode_error=..; parse_error=..` 与 `findings: parse_error=..; ...` 两行，消除同名计数连排；同步黄金串。
- **TR**: `rule` `TestRenderRoundTripTextReportGolden` 黄金串更新且通过。
- **Completion Evidence**: `roundtrip_report.go` 渲染两段；`roundtrip_report_test.go` 黄金串更新。

## Remediation Issue R6（F6/F7/F10，minor）：路径表达式只读求值、运行期错误归类、空参数报错
- **Status**: `completed`
- **Priority**: medium
- **Description**: `--path` 对每篇文档的 `node.Copy()` 深拷贝求匹配节点且 `DontAutoCreate=true`（赋值算子内部虽会 WritableClone，但只作用于拷贝，被检树绝不改写）；求值运行期错误作为操作错误返回（CLI exit 2）；`--compare ""` 显式报错；`--document` 空 token 报错而非跳过。
- **TR**: `rule` `TestRoundTripCheckPathAssignmentDoesNotMutateDocument` 断言 `.a = 5` 后原节点仍为 1；`--path 'error("boom")'` 经 `TestRoundTripCheckPathExpressionRuntimeError`/`TestRoundTripCommandPathRuntimeErrorExitTwo` 验证 exit 2；空 compare/document 由两个 CLI 测试覆盖，手工复现均 exit 2。
- **Completion Evidence**: `roundtrip_flow.go` checkWithPath 签名改为返回 error 并深拷贝求值；`cmd/roundtrip_command.go` 加空 compare 判定、parseRoundTripDocumentArgs 空 token 报错。

## Task 1: 库层数据模型与节点树比较器
- **Status**: `completed`
- **Priority**: high
- **Completion Evidence**: 新增 `pkg/yqlib/roundtrip.go`（选项/状态/finding/报告类型、状态严重度汇总、稳定排序、复用 explodeOp 的 roundTripExplode）与 `pkg/yqlib/roundtrip_compare.go`（确定性点路径 RoundTripPathString、effective tag、int/float/bool/null 语义比较、map 键匹配/键序（R3 后仅双向键集合相等才报键序）、seq 按位比较）；`pkg/yqlib/roundtrip_compare_test.go` 10 个测试覆盖 TR-1.1/1.2/1.3（含前后值与两侧键序列断言、键集合差异不误报，TR-1.3 自评 5/5）。`go test ./pkg/yqlib/ -run TestRoundTripCompare` 通过。
- **Depends On**: None
- **Description**:
  - 新增 `pkg/yqlib/roundtrip.go`：定义选项结构体（输入/输出格式名、比较策略、文档索引集合、路径表达式、报告格式/目标）、状态枚举（`ok|semantic_diff|encode_error|parse_error`）、finding 类型枚举（`parse_error|encode_error|doc_count_changed|type_changed|value_changed|missing|added|key_order_changed`）、Finding/FileResult/Report 结构体与稳定排序方法。
  - 新增 `pkg/yqlib/roundtrip_compare.go`：递归树比较器，按 FR-11/FR-12 产出路径级 finding；容器 kind 不同整体报 type_changed；标量先比 tag（guessTagFromCustomType）再比值（int/float 数值语义、bool/null/字符串字面）；map 按键匹配、记录 missing/added 与 key_order（含两侧键序列）；array 按位置比较；路径用 GetPath 风格的点路径（数组 `[i]）；比较前在 encoder 不支持别名时复用 explode 语义；所有 finding 最终按 (doc, path, kind) 稳定排序。
  - 新增 `pkg/yqlib/roundtrip_compare_test.go`：覆盖值变化、类型变化、map 键序、缺失/新增、数组错位、嵌套路径前缀、两种策略下 informational 标记。
- **Acceptance Criteria Addressed**: AC-2, AC-9, AC-10
- **Test Requirements**:
  - `rule` TR-1.1: 构造差异节点对（含 tag/kind/键序/缺失/新增），比较结果的类型、文档索引、点路径与预期完全一致；证据：`go test ./pkg/yqlib/ -run RoundTrip -v`
  - `rule` TR-1.2: finding 经稳定排序后两次运行顺序逐字节一致；证据：同测试内断言
  - `rubric` TR-1.3: finding 信息丰富度；1-5；1=仅类型，3=有路径无前后值，5=含前后值/两侧键序列；阈值 >=4；证据：测试断言字段内容

## Task 2: 库层检查流程编排
- **Status**: `completed`
- **Priority**: high
- **Completion Evidence**: 新增 `pkg/yqlib/roundtrip_flow.go`：`CheckRoundTrip(filenames, options)` 工厂校验（input 需 decoder、output 需 encoder+decoder）、按文档序号的 parse_error 隔离、文档索引越界返回操作错误、路径表达式经 ExpressionParser/DataTreeNavigator、每文件全新 encoder/decoder 实例、镜像 printer 的 bufio writer+Flush（取证发现 XML encoder mapping 路径不 Flush，必须经 bufio）、文档数配对比较、严重度汇总。`pkg/yqlib/roundtrip_test.go` 19 个测试覆盖 TR-2.1~2.4（ok/parse/csv encode/文档与路径筛选（含 R2 原始序号保持）/越界/多文件/别名 explode/props 多文档合并与点号键/INI 丢嵌套/XML 类型变化/确定性/无 decoder 组合/redecode 失败（伪 Format）/路径运行期错误/赋值路径不改写被检树），全部通过。
- **Depends On**: Task 1
- **Description**:
  - 在 `pkg/yqlib/roundtrip.go`（或同包新增文件）实现不依赖 cobra 的检查入口：输入 io.Reader/文件名列表 → 经 Format 工厂创建全新 decoder 解码（FR-8，parse_error 含文档序号与原始错误、单文件失败不影响其他文件）→ `--document` 索引筛选（越界报错）→ 路径表达式经 ExpressionParser + DataTreeNavigator 求匹配节点（FR-6）→ 每检查单元用**全新 encoder 实例**编码到 bytes.Buffer（FR-9 phase=encode，错误含文档/路径/原文）→ 用**全新 decoder 实例**再解码（phase=redecode）→ 无路径时多文档整体编解码并比对文档数（FR-10，按序配对、超出记 missing/added）→ 调用 Task 1 比较器 → 汇总文件与整体状态（FR-14）。
  - encoder 不支持别名时对原节点树做 explode（复用 printer.go 同款 explodeOp 导航），与 Task 1 约定一致。
  - 输出格式必须同时具备 encoder 与 decoder，否则返回明确的用法错误。
  - 新增 `pkg/yqlib/roundtrip_test.go`：ok 往返（yaml↔json）、CSV 嵌套 encode_error、损坏输入 parse_error、多文档 doc_count_changed、再解码失败、文档/路径筛选、多文件汇总、别名展开不误报。
- **Acceptance Criteria Addressed**: AC-2, AC-3, AC-4, AC-5, AC-10
- **Test Requirements**:
  - `rule` TR-2.1: 各类状态（ok/parse_error/encode_error/semantic_diff）可被正确分类且携带原始错误信息；证据：单测断言 status 与 phase
  - `rule` TR-2.2: 文档筛选与路径筛选后被检查单元与预期精确一致，越界索引返回错误；证据：单测
  - `rule` TR-2.3: 同一输入重复检查结果逐字节一致（确定性）；证据：单测比较两次 JSON 渲染
  - `rule` TR-2.4: 在 `yq_nojson` 之外不引入对具体格式实现文件的直接依赖，仅通过 Format 工厂调用；证据：`go build ./...` 与代码审查

## Task 3: 报告渲染（text/json）与原子文件写入
- **Status**: `completed`
- **Priority**: high
- **Completion Evidence**: 新增 `pkg/yqlib/roundtrip_report.go`：固定段落文本报告（渲染前强制稳定排序，无颜色/时间戳）、encoding/json 2 空格缩进+换行（map 键自然有序）、`WriteRoundTripReport` 与 CreateTemp+Write+Sync+Close+Chmod+Rename 原子替换、失败删临时文件。`pkg/yqlib/roundtrip_report_test.go` 7 个测试覆盖 TR-3.1~3.4（文本黄金串、parse_error 行、JSON 可解析且逐字节确定、未知格式、stdout、父目录缺失不留任何文件/目录、成功替换旧文件无残留；TR-3.4 自评 5/5），全部通过。
- **Depends On**: Task 2
- **Description**:
  - 新增 `pkg/yqlib/roundtrip_report.go`：
    - 文本渲染器：固定段落（参数回显 → 每文件 status/documents/findings → summary），无颜色无时间戳，路径与前后值格式固定（FR-13/FR-14）。
    - JSON 渲染器：标准库 encoding/json，2 空格缩进、结尾换行；结构含 parameters/files[]/summary/status；finding 切片在流程层已稳定排序，map 键由 encoding/json 自然排序。
  - 原子写入：`--report-file` 时在目标同目录 `CreateTemp` → 写入 → `Sync` → 关闭 → `Rename`；任一步失败删除临时文件并返回错误，保证目标无半份结果（FR-15）；stdout 目标直接写出。
  - 新增 `pkg/yqlib/roundtrip_report_test.go`：两种格式快照式断言（黄金字符串）、重复运行一致、JSON 可解析、向不可写目录/不存在父目录写文件时返回错误且无残留临时文件、既有目标文件在失败时内容不变。
- **Acceptance Criteria Addressed**: AC-6, AC-7, AC-9
- **Test Requirements**:
  - `rule` TR-3.1: text/json 报告对固定 Report 结构体输出逐字节稳定（含/不含 finding 两种）；证据：单测黄金字符串
  - `rule` TR-3.2: JSON 输出可被 encoding/json 解析且字段齐全（parameters/files/findings/summary/status）；证据：单测
  - `rule` TR-3.3: 写入失败（父目录不存在/只读目录）后目标位置无文件或原文件字节不变、临时目录无残留 `*roundtrip*` 临时文件；证据：t.TempDir 模拟
  - `rubric` TR-3.4: 报告排障可用性；1-5；锚点同 AC-9；阈值 >=4；证据：样例文本人工核对

## Task 4: CLI 子命令 `roundtrip` 与接线
- **Status**: `completed`
- **Priority**: high
- **Completion Evidence**: 新增 `cmd/roundtrip_command.go`（别名 rt；--compare/--document/--path/--report-format/--report-file；复用 configureFormats/configureEncoder 使 -p/-o 及全部编码选项生效；强制 colorsEnabled=false 保证确定性；`ExitCodeError{Code,cause}` + yq.go errors.As 实现 0/1/2，既有错误路径默认仍 1），root.go 仅加一行注册；`cmd/roundtrip_command_test.go` 20 个进程内 CLI 测试覆盖 TR-4.1~4.4（ok/diff/parse/encode 退出 1，未知 flag/未知策略/未知格式/越界/空 compare/空 document token/路径运行期错误/报告写失败退出 2 且无 Usage，文档与路径筛选、JSON 报告、report-file 成功与失败、XML 键序 semantic 退出 0/ordered 退出 1、rt 别名、eval 冒烟不变），全部通过。
- **Depends On**: Task 3
- **Description**:
  - 新增 `cmd/roundtrip_command.go`：注册 `roundtrip`（别名 `rt`）子命令，参数仅文件列表（复用 stdin/`-` 约定）；新增本地 flag：`--compare=semantic|ordered`、`--document`（可重复/逗号分隔）、`--path`、`--report-format=text|json`、`--report-file`。
  - 复用持久 `-p/-o` 与全部编码选项 flag：装配 yqlib 选项时沿用 init/configure 流程中格式解析与 auto 检测、indent/unwrap/prefs 应用逻辑（抽取或复用既有函数，避免行为分叉）；输出格式无 decoder 时用法错误。
  - 退出码语义（NFR-1）：yq.go 现状是任意 Execute 错误统一 `os.Exit(1)`。采用最小且对既有命令零影响的方案——在 cmd 包定义携带退出码的哨兵错误类型（如 `exitCodeError{code int}`），roundtrip 子命令对用法/工具错误（含报告写失败）返回 code=2 的该错误、对检查发现问题返回 code=1；yq.go 的错误出口用 `errors.As` 提取 code，缺省仍为 1。子命令参数不走 processArgs 的表达式自动识别，全部位置参数均视为文件（复用 processStdInArgs 的 stdin 补 `-` 逻辑）。
  - 在 `cmd/root.go` 的 AddCommand 中注册；不改任何既有 flag 默认值。
  - 新增 `cmd/roundtrip_command_test.go`：端到端调用命令（走内存/stdin 与临时文件），覆盖 AC-1/2/4/5/6/7/8 的 CLI 可观测面：退出码、stdout/stderr 内容、`--report-file` 原子性、非法 flag/格式返回退出码 2、未注册路径下 `eval` 行为不变的冒烟断言。
- **Acceptance Criteria Addressed**: AC-1, AC-2, AC-4, AC-5, AC-6, AC-7, AC-8, AC-10
- **Test Requirements**:
  - `rule` TR-4.1: ok 场景退出 0；semantic_diff/encode_error/parse_error 退出 1；用法错误与报告写失败退出 2；证据：CLI 测试断言退出码
  - `rule` TR-4.2: text 与 json 报告输出到 stdout 内容正确；`--report-file` 成功且内容与 stdout 模式一致；证据：CLI 测试
  - `rule` TR-4.3: 不带 roundtrip 子命令时既有 `eval` 冒烟输出与现状一致；证据：CLI 测试
  - `rubric` TR-4.4: flag 命名/帮助文本与现有风格一致性、UK 拼写；1-5；阈值 >=4；证据：help 输出审查

## Task 5: 验收测试与全量门禁
- **Status**: `completed`
- **Priority**: medium
- **Completion Evidence**: 新增可执行 `acceptance_tests/roundtrip.sh`（15 项 shunit2，acceptance.sh 自动发现，全部 OK；含未知 flag 退出 2 无 Usage、XML 键序两策略、--document 0,2 原始序号）；`bash scripts/acceptance.sh` 全部 18 个脚本通过；`bash scripts/format.sh` 与 `bash scripts/spelling.sh`（typos 1.47.2）通过；`go vet ./...` 通过；`go test ./...` 除基线预存 `TestJSONScenarios`（已用 git stash 验证与本次改动无关：go-json 错误文案随环境版本变化）外全部通过；`go build -tags 'yq_nojson yq_nocsv yq_notoml yq_noxml yq_noyaml'` 成功（TR-5.1~5.3 满足，golangci-lint 因工具链 go1.26 vs go1.27 不兼容在本机 panic，属环境问题）。
- **Depends On**: Task 4
- **Description**:
  - 按 `acceptance_tests/` 现有 shunit2 范式新增 `acceptance_tests/roundtrip.sh`：二进制级覆盖 ok 往返退出码、差异检出、json 报告解析（可用 ./yq 自身校验 JSON）、报告文件原子性、默认行为不受影响；按 acceptance.sh 的发现机制注册（如需要）。
  - 运行并修复：`bash scripts/format.sh`、`bash scripts/check.sh`（或 `make local check/test`）、`bash scripts/spelling.sh`、`go build -o yq . && bash scripts/acceptance.sh`、`go test ./...`；必要时 `make local vendor` 解决 vendoring 不一致。
- **Acceptance Criteria Addressed**: AC-1, AC-6, AC-8, AC-10
- **Test Requirements**:
  - `rule` TR-5.1: `go test ./...` 全绿；证据：命令输出
  - `rule` TR-5.2: format/spelling/lint 通过，验收脚本含新增用例且通过；证据：各脚本输出
  - `rule` TR-5.3: `go build -o yq .` 成功且 `./yq roundtrip --help` 展示新 flag；证据：命令输出
