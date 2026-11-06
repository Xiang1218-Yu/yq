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
