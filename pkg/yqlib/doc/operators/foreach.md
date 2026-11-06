# Foreach

Like [ireduce](reduce.md), foreach processes a collection of items in order while maintaining a rolling state (the accumulator). Unlike ireduce, foreach outputs a result for every item in the collection.

```
<exp> as $<name> foreach (<init>; <update>; <extract>)
```

The two part form is also available, where the output is the accumulator itself (equivalent to an extract expression of `.`):

```
<exp> as $<name> foreach (<init>; <update>)
```

- `<exp>` is any expression that produces the items to loop over (e.g. `.[]`).
- `<init>` is evaluated once to initialise the accumulator. When using `eval` this happens independently for each document; when using `eval-all` there is a single accumulator shared across every input file and document, processed in file and document order.
- `<update>` is evaluated for each item in order, with `.` set to the current accumulator and `$<name>` set to the current item.
- `<extract>` is evaluated against the updated accumulator after each item, and its results are output.

The syntax mirrors jq's `foreach` expression and follows the same semantics.

## Rolling state, output the state each step
Given a sample.yml file of:
```yaml
- 10
- 2
- 5
- 3
```
then
```bash
yq '.[] as $item foreach (0; . + $item)' sample.yml
```
will output
```yaml
10
12
17
20
```

## Use an extract expression to output something different to the state
The third expression is evaluated against the updated accumulator each step.

Given a sample.yml file of:
```yaml
- 10
- 2
- 5
- 3
```
then
```bash
yq '.[] as $item foreach (0; . + $item; . * 2)' sample.yml
```
will output
```yaml
20
24
34
40
```

## Empty collection produces no output
Given a sample.yml file of:
```yaml
[]
```
then
```bash
yq '.[] as $item foreach (0; . + $item)' sample.yml
```
will output
```yaml
```

## Single value input
Given a sample.yml file of:
```yaml
5
```
then
```bash
yq '. as $item foreach (0; . + $item)' sample.yml
```
will output
```yaml
5
```

## Any multi-node expression can be used as the input
Running
```bash
yq --null-input '(1, 2, 3) as $item foreach (0; . + $item)'
```
will output
```yaml
1
3
6
```

## eval-all shares one rolling state across all documents
When run with eval-all, the documents are processed together in file and document order, with a single accumulator. With eval (not shown), the state is reset for each document.

Given a sample.yml file of:
```yaml
x: 1
```
And another sample another.yml file of:
```yaml
x: 2
```
then
```bash
yq eval-all '.x as $item foreach (0; . + $item)' sample.yml another.yml
```
will output
```yaml
1
3
```

## Build up an object, each step emits the current state
The update mutates the accumulator in place; foreach takes a copy each step so the earlier outputs are not affected by later updates.

Given a sample.yml file of:
```yaml
- name: Cathy
  has: apples
- name: Bob
  has: bananas
```
then
```bash
yq '.[] as $item foreach ({}; .[$item.name] = $item.has)' sample.yml
```
will output
```yaml
Cathy: apples
Cathy: apples
Bob: bananas
```

## Comments and styles are preserved in each output
Given a sample.yml file of:
```yaml
- cat # meow
- dog # woof
```
then
```bash
yq '.[] as $item foreach ([]; . + [$item])' sample.yml
```
will output
```yaml
- cat # meow
- cat # meow
- dog # woof
```

