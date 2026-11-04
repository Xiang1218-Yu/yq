# yq 离线 Round-Trip 检查流程 - 产品需求文档

## Overview
- **Summary**: 为 yq 新增一个离线 round-trip（解码 → 重新编码 → 再解码）检查流程，以独立子命令 `yq roundtrip`（别名 `rt`）提供。用户可指定输入格式、输出（中间）格式、编码选项、比较策略、文档筛选与路径筛选；工具报告路径级别的值变化、类型变化、键顺序变化、文档数量变化以及无法表示的节点，并输出确定性的文本或 JSON 报告。
- **Purpose**: 在不依赖外部工具的前提下，帮助用户评估"把文档从 A 格式转换为 B 格式"的信息保真度：哪些值/类型会变、键序是否保留、文档条数是否变化、哪些结构是目标格式无法表示的。
- **Target Users**: 使用 yq 做跨格式转换（YAML/JSON/CSV/XML/properties/INI/TOML/HCL/Lua/base64/uri 等）的开发者与运维人员；CI 流水线中需要对转换保真度做门禁检查的用户。

## Goals
- 新增独立子命令完成"输入解码 → 输出格式编码 → 输出格式再解码 → 内部节点树比较"的完整离线流程。
- 明确区分三类结果：**解析失败**（输入无法解码）、**编码失败**（目标格式无法表示节点，或编码器产出无法被其自身解码器解析）、**往返语义变化**（流程完成但节点树存在差异）。
- 路径级别报告：值变化、类型变化、键顺序变化、缺失/新增节点；文件级别报告：文档数量变化。
- 支持只检查指定文档（0 基索引列表）和指定路径（yq 路径表达式）。
- 提供确定性的文本与 JSON 两种报告输出（无时间戳、无颜色、固定排序）。
- 报告写文件采用"临时文件 + fsync + 原子重命名"，写入失败时目标位置不出现半份结果。
- 未启用该流程（不使用新子命令）时，现有 `eval`/`eval-all` 的格式转换、输出行为、退出码完全不变。

## Non-Goals
- 不检查注释、锚点/别名、字面样式（quote/literal/folded）等表现层信息的保真度。
- 不做字节级（byte-identical）输出比对。
- 不修改任何输入文件，不与第二个参考文件做比对。
- 不新增编码器/解码器、不修改任何现有编码器/解码器的行为。
- 不修改 `candidate_node.go` 或核心导航/求值逻辑。
- 不提供网络获取、schema 校验等能力。

## Background & Context
- 格式注册于 `pkg/yqlib/format.go`，`Encoder`（`pkg/yqlib/encoder.go`）与 `Decoder`（`pkg/yqlib/decoder.go`）均为可反复实例化的工厂接口：`Format.GetConfiguredEncoder()`、`Format.DecoderFactory()`。
- 现有解码工具：`readDocuments(reader, filename, fileIndex, decoder)`（`pkg/yqlib/utils.go`）可把一个输入流解码为 `*list.List` 的 `CandidateNode`（每节点带 `document`/`filename`/`fileIndex`）。
- 现有编码路径：`Printer.PrintResults`（`pkg/yqlib/printer.go`）按文档边界调用 `encoder.PrintDocumentSeparator` 与 `encoder.Encode(writer, node)`；编码器对不能处理别名的格式会先 explode。
- 各格式的表示能力差异（已核实）：CSV 仅接受标量数组行/对象数组，形状不符时返回 error；properties 对不支持的节点返回 `unsupported node`；INI 根必须是 MappingNode 且嵌套非标量会被静默丢弃；XML 顶层仅 Mapping/Scalar；JSON/YAML 键序随 `CandidateNode.Content` 保留。这些差异正是 round-trip 检查要暴露的对象。
- CLI 基于 cobra：`cmd/root.go` 注册持久 flag（`-p/-o/--indent/--csv-*/--xml-*/--ini-*` 等）与 `eval`、`eval-all` 子命令；新增子命令可直接复用全部既有持久 flag 作为"编码选项"。
- `CandidateNode.GetPath()/GetNicePath()` 可生成稳定路径；`guessTagFromCustomType()` 与 `compareScalars`（`pkg/yqlib/operator_compare.go`）提供标量语义比较基础（int/float 数值语义、str、null、bool）。

## Functional Requirements

### 命令与参数
- **FR-1**: 新增子命令 `yq roundtrip [file ...]`（别名 `rt`）；无文件参数或文件为 `-` 时从 stdin 读取（与现有约定一致）；可传入多个文件，逐文件检查并汇总。
- **FR-2**: 复用持久 flag `-p/--input-format`、`-o/--output-format`（含 auto 检测：input 默认按扩展名、output 默认跟随 input）作为输入格式与中间编码格式；二者均必须拥有可用的 decoder（输入格式）与 encoder+decoder（输出格式，因为需要再解码），不满足时给出明确用法错误。
- **FR-3**: 既有编码选项 flag（`--indent/-I`、`--csv-separator`、`--csv-auto-parse`、`--xml-*`、`--ini-*`、`--properties-*`、`--lua-*`、`--yaml-*` 等）原样作用于中间编码步骤。
- **FR-4**: 新增 `--compare` 比较策略，取值 `semantic`（默认）与 `ordered`：
  - `semantic`：映射按键匹配、忽略键顺序；标量按类型+语义值比较（int/float 数值语义、bool、null、字符串逐字）；数组按位置逐元素比较。
  - `ordered`：在 `semantic` 基础上，把同键集合下的键顺序变化也计为失败。
- **FR-5**: 新增 `--document <spec>`（可重复或逗号分隔的 0 基索引，如 `0,2`）筛选要检查的文档；缺省检查全部文档；越界索引产生明确错误。
- **FR-6**: 新增 `--path <yq 表达式>` 限定检查范围（如 `.a.b`、`.items[]`）；对每个匹配节点独立做 encode→decode→compare，报告路径以匹配节点在原文档中的完整路径为前缀；缺省等价于根节点（每文档一个检查单元）。指定 `--path` 时不做文档数量变化检查（检查单元为子树而非文档流）。
- **FR-7**: 新增 `--report-format text|json`，默认 `text`；新增 `--report-file <path>`，缺省写 stdout。

### 检查流程
- **FR-8（解析失败）**: 输入流打开成功但解码任一文档失败时，记录该文件的 `parse_error` 结果（含文件名、文档序号、解码器原始错误信息），该文件跳过后续步骤；其余文件继续检查。输入文件本身无法打开属于工具/用法错误（见 NFR 退出码），不是 parse_error。
- **FR-9（编码失败）**: 对每个检查单元，用输出格式的全新 encoder 实例编码到内存 buffer：
  - 编码返回错误 → 记录 `encode_error`（phase=`encode`），含义为存在无法表示的节点；附带文档索引、路径前缀与编码器原始错误信息；
  - 编码成功但用全新 decoder 实例无法解析编码结果 → 记录 `encode_error`（phase=`redecode`）；
  - 发生 encode_error 的检查单元不参与树比较。
- **FR-10（文档数量变化）**: 无 `--path` 时，把全部入选文档顺序编码进同一 buffer（文档间调用 `PrintDocumentSeparator`，与正常多文档输出一致），再整体解码；比较"入选文档数"与"再解码文档数"，不一致时记录文件级 `doc_count_changed`（expected/actual）；树比较按下标配对，超出一侧长度的文档整体记为 missing/added。
- **FR-11（路径级树比较）**: 对配对成功的两侧节点做递归比较，产生以下 finding 类型，均带文档索引与点路径（数组用 `[i]`）：
  - `type_changed`：标量 tag 不同（如 `!!int`→`!!str`）或容器 kind 不同（map↔array↔scalar）；
  - `value_changed`：同类型标量语义值不同（数值按数值语义，其余按字面）；
  - `missing`：路径只存在于转换前；
  - `added`：路径只存在于转换后；
  - `key_order_changed`：映射键集合相同但顺序不同（记录两侧键序列）。
- **FR-12（别名处理）**: 当输出格式 encoder 不能处理别名（`CanHandleAliases()==false`）时，比较前的原节点树按与 `Printer` 相同的 explode 语义展开，避免把别名展开误报为差异。
- **FR-13（确定性输出）**:
  - 文本报告：固定段落顺序、无颜色、无时间戳；同一输入与选项重复运行输出逐字节一致；finding 按 (文件名, 文档索引, 路径, 类型) 稳定排序；
  - JSON 报告：单个 JSON 对象（UTF-8、2 空格缩进、结尾换行），包含 parameters（input_format/output_format/compare/path/documents）、files[]（filename、status、documents_before/after、findings[]）、summary（各状态文件数与各类型 finding 计数）与整体 status；映射键按字典序、finding 按稳定顺序，保证确定性。
- **FR-14（状态汇总）**: 文件状态与整体状态取值 `ok | semantic_diff | encode_error | parse_error`，严重度 parse_error > encode_error > semantic_diff > ok；key_order 在 `semantic` 策略下标记为 informational（不抬高状态、不影响退出码），在 `ordered` 策略下作为失败。
- **FR-15（原子报告写入）**: 指定 `--report-file` 时，先写同目录临时文件，完整写入并 fsync 成功后再 rename 覆盖目标；任何步骤失败则删除临时文件并返回错误，目标路径的既有内容保持不变、不存在半份文件。
- **FR-16（零影响）**: 不修改 `eval`/`eval-all` 的任何既有行为与退出码；roundtrip 逻辑仅在新子命令内可达；公共库代码以新增文件为主，对既有文件的改动仅限于子命令注册等接线代码。

## Non-Functional Requirements
- **NFR-1（退出码）**: `0` = 未发现失败（semantic 策略下仅 informational 键序变化仍为 0）；`1` = 检查完成但发现任一 parse/encode/语义失败；`2` = 工具或用法错误（未知 flag、未知格式、文件无法打开、报告写入失败等），此时不保证报告内容，但保证不留半份报告文件。
- **NFR-2（纯离线）**: 流程只依赖内存 buffer 与本地文件系统，不发起任何网络/子进程调用。
- **NFR-3（测试）**: 为库层比较/流程与 CLI 层分别补充测试，覆盖：成功往返、值变化、类型变化、键顺序（两种策略）、文档数变化、解析失败、编码失败（CSV 嵌套等）、再解码失败、文档筛选、路径筛选、多文件汇总、JSON/文本确定性（含重复运行逐字节一致）、原子写失败不留半份；遵循仓库 UK 英文拼写与 format/check/spelling 要求。
- **NFR-4（可复用库 API）**: 核心流程放在 `pkg/yqlib` 新增文件中，以不依赖 cobra 的类型/函数暴露（选项结构体 + 检查入口 + 报告渲染），CLI 仅做参数装配。
- **NFR-5（构建标签兼容）**: 新增代码必须在 `yq_nojson` 等最小构建标签下可编译（不得无保护地直接依赖 json encoder/decoder 具体实现；通过 Format 工厂调用）。

## Constraints
- **Technical**: Go（见 go.mod）；仅使用既有 Encoder/Decoder/Printer/Navigator 公共模式；不得修改 `candidate_node.go`；格式特有逻辑留在各自 encoder/decoder 文件内；遵循 AGENTS.md 的编码/测试/拼写规则。
- **Business**: 不执行 git add/commit；不做任何 GitHub 操作。
- **Dependencies**: 仅使用 go.mod 既有依赖（JSON 渲染可用标准库 encoding/json 以保证键序确定）。

## Assumptions
- "编码选项"指 yq 已有的全部格式相关持久 flag；不为 round-trip 单独发明新的编码参数。
- `--document` 筛选的索引是"输入文档流中的 0 基序号"，在路径表达式求值之前应用。
- 比较的语义值以再解码后的 tag/value 为准：两侧 tag 不同即 type_changed，不再把同字面量当作相等。
- stdin 作为输入文件时文件名为 `-`；stdin 不参与按扩展名的 auto 检测（默认 yaml，与现有行为一致）。
- 报告 JSON 是工具自身的产物而非 yq 数据文档，使用标准库序列化以保证确定性，不经过 yq JSON encoder。

## Acceptance Criteria

### AC-1: 成功往返报告 ok 且退出码 0
- **Type**: `rule`
- **Given**: 一个语义可被输出格式完整表示的输入文件（如简单 YAML → JSON）
- **When**: 运行 `yq roundtrip -p yaml -o json file.yaml`
- **Then**: 文件状态为 `ok`，无 finding，退出码 0；文本报告含参数回显与 summary
- **Pass Condition**: 退出码 0 且文本报告中该文件 `status: ok`
- **Evidence**: 新增 CLI 测试用例与命令实际输出

### AC-2: 值/类型/键序变化被路径级报告
- **Type**: `rule`
- **Given**: 会触发已知损失的输入与格式对（利用现有编码器行为构造，如 INI 静默丢弃非标量嵌套、JSON 数字/字符串类型变化、映射键序差异）
- **When**: 分别以 `--compare semantic` 与 `--compare ordered` 运行
- **Then**: 报告中出现带文档索引与点路径的 `value_changed`/`type_changed`/`missing` 与 `key_order_changed`；semantic 下键序为 informational，ordered 下计为失败
- **Pass Condition**: 两种策略下 finding 类型、路径、informational 标记均符合预期，退出码分别为 0（仅键序）与 1
- **Evidence**: 库层比较测试 + CLI 测试

### AC-3: 文档数量变化
- **Type**: `rule`
- **Given**: 含多个文档的 YAML 流编码为不支持/合并多文档的输出格式场景
- **When**: 运行 roundtrip 检查
- **Then**: 出现文件级 `doc_count_changed` 并给出 expected/actual，状态为 `semantic_diff`，退出码 1
- **Pass Condition**: finding 的 expected/actual 与实际文档数一致
- **Evidence**: 库层测试

### AC-4: 解析失败与编码失败明确区分
- **Type**: `rule`
- **Given**: (a) 语法损坏的输入；(b) 结构无法被目标格式编码的输入（如嵌套数组 → CSV）
- **When**: 运行 roundtrip
- **Then**: (a) 文件状态 `parse_error`，finding 含解码器原始错误；(b) 文件状态 `encode_error`、phase=`encode`，含编码器原始错误；其他文件不受影响；退出码均为 1
- **Pass Condition**: 两类结果在文本与 JSON 报告中可机器区分，且不会被误报为 semantic_diff
- **Evidence**: 库层 + CLI 测试

### AC-5: 文档与路径筛选
- **Type**: `rule`
- **Given**: 多文档、含多个子路径的输入
- **When**: 使用 `--document 0,2` 与 `--path '.items[]'` 等组合运行
- **Then**: 仅入选文档/匹配节点被检查；finding 路径带原文档完整路径前缀；越界文档索引返回用法错误（退出码 2）
- **Pass Condition**: 被检查单元集合与筛选条件精确一致
- **Evidence**: 库层 + CLI 测试

### AC-6: 确定性文本与 JSON 输出
- **Type**: `rule`
- **Given**: 固定输入与选项
- **When**: 连续运行两次分别取 text/json 报告
- **Then**: 两次输出逐字节一致；JSON 可被标准 JSON 解析器解析，键序固定、无时间戳/随机字段；text 无颜色码
- **Pass Condition**: `run1 == run2`（两种格式各一次）且 JSON 解析成功
- **Evidence**: 测试中字节比较断言

### AC-7: 报告原子写入
- **Type**: `rule`
- **Given**: 指定 `--report-file`；并构造写入失败场景（目标目录不可写/父路径不存在）
- **When**: 运行检查
- **Then**: 成功时报告经临时文件 rename 产生；失败时退出码 2、目标位置无新增文件或既有文件内容不变、临时文件被清理
- **Pass Condition**: 失败场景后目标路径无半份结果、无残留临时文件
- **Evidence**: 临时目录模拟的 CLI/库测试

### AC-8: 现有行为零影响
- **Type**: `rule`
- **Given**: 未使用 roundtrip 子命令的既有调用方式
- **When**: 运行既有单元/验收测试套件
- **Then**: 全部通过；`eval`/`eval-all` 的输出与退出码不变
- **Pass Condition**: `go test ./...` 与验收脚本相关用例通过
- **Evidence**: CI 命令输出

### AC-9: 报告可读性与信息完整度
- **Type**: `rubric`
- **Dimension**: 报告对排障的有用程度
- **Scale**: 1-5
- **Anchors**: 1 = 只有"通过/失败"，无法定位；3 = 有路径和类型但缺少前后值/错误原文；5 = 每类 finding 均带文档、路径、前后值/键序列/原始错误，summary 可一眼定位问题文件
- **Pass Threshold**: >= 4
- **Evidence**: 文本/JSON 报告样例与测试断言

### AC-10: 实现与仓库约定的契合度
- **Type**: `rubric`
- **Dimension**: 代码遵循仓库架构（工厂接口、format.go 注册模式、测试范式、UK 拼写、最小构建标签兼容）
- **Scale**: 1-5
- **Anchors**: 1 = 改动核心文件或为 roundtrip 特判既有逻辑；3 = 功能可用但接线生硬；5 = 仅新增文件+最小注册改动，复用既有工厂/explode/路径工具，测试遵循现有范式
- **Pass Threshold**: >= 4
- **Evidence**: 代码审查与 `make local` 格式/检查结果

## Open Questions
- 无（流程中如遇格式行为的不确定点，以现有 encoder/decoder 实际行为与测试证据为准，不臆造行为）。
