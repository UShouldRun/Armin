---
name: js-style
description: The user's JavaScript style — 2-space, semicolons, double quotes, aligned "=" / "from" / object values, padded ${ x }, arrow functions. Load before writing or editing JS or TS (non-markup).
---
## Basics
- 2-space indentation, semicolons, double quotes.
- Arrow functions for components, hooks and helpers:
  `const useEvents = () => { ... };`, with `export default` at the end.
- A space on both sides of every binary operator: `a + b`, `i * 2`,
  `x === y`, `count = count + 1`. Never `a+b` or `x=y`.

## Alignment
- Align the `=` of consecutive declarations:

  ```js
  const { request }  = useApi();
  const [ok, setOk]  = useState(false);
  const navigate     = useNavigate();
  ```
- Align the `from` of consecutive imports:

  ```js
  import toast        from "react-hot-toast";
  import { useState } from "react";
  ```
- Align object-literal values within a group:

  ```js
  const row = {
    date:     formatDate(event.date),
    place:    event.place,
    attended: event.count,
  };
  ```

## Expressions
- Template interpolations padded: `${ value }`.
- Spaced braces in single-line objects and destructuring:
  `{ a, b }`, `{ id: 1 }`.

## Errors and logging
- try / catch / finally with a blank line before `} catch` and
  `} finally`:

  ```js
  try {
    const data = await request("/api/events");
    setEvents(data);

  } catch (error) {
    console.error("[ERROR]: useEvents - ", error);

  } finally {
    setLoading(false);
  }
  ```
- Logs as `console.error("[ERROR]: functionName - ", error)`.

## Blank lines between blocks
- A blank line after every closing `}` of an `if` / `for` / `switch`
  / function when more code follows at that level (not before
  `else`, `catch`, `finally` — those get the blank line *inside*,
  per the try/catch rule — or another `}`).
- A blank line before a `return` that follows a group of statements.

  ```js
  if (!res.ok) {
    throw new Error(await res.text());
  }

  const data = await res.json();
  setRows(data);

  return data;
  ```

## Before finishing
Re-read the diff for missing blank lines between blocks, unaligned `=` / `from` groups, unpadded `${x}`,
unspaced operators, single quotes in new code.
