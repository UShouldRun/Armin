---
name: c-style
description: The user's C style and repo layout — lib/<name>/{include,src,build} with public and _private headers, u8..u64 typedefs, __module_ private functions, column-aligned prototypes, packed bit encodings, arenas over malloc, makefile static libs. Load before writing or editing C.
---
## Repo layout
```
makefile
README.md
LICENSE
src/main.c                  the program; thin
lib/<name>/include/<name>.h          public API
lib/<name>/include/<name>_private.h  internals: structs, private prototypes
lib/<name>/src/<name>.c
lib/<name>/build/           objects and lib<name>.a (not committed)
test/<name>/t<name>.c       one test program per library
docs/                       design notes (encodings, grammars)
```
- Each library builds as a static `lib<name>.a`. Reusable libraries
  live in their own repo with `install` / `uninstall` targets copying
  to `/usr/local/lib` and `/usr/local/include/<name>/`, and are linked
  with `-l<name>`.
- makefile: `CC = gcc`, `CFLAGS = -Wall -Wextra -Werror -Iinclude`
  (`-g` while developing, `-O2` for release), `$(wildcard ...)` /
  `$(patsubst ...)` for sources and objects, `.PHONY`, `clean` removes
  build dirs. Commented sections (`# Compiler and flags`,
  `# Directory structure`).

## Headers
- Guards: `#ifndef __NAME_H__` / `#define __NAME_H__` /
  `#endif // !__NAME_H__`.
- Public header: includes, a doc block, opaque types, prototypes.
  Structs are opaque — `typedef struct arena Arena;` in the public
  header, `struct arena { ... };` in `_private.h`. Some codebases
  typedef the pointer instead (`typedef struct chess_moves
  *ChessMoves;`); match whichever the codebase already uses.
- Private header includes the public one, defines the structs and
  declares every internal function.
- Doc blocks say what, when to use it, and time/space complexity:

  ```c
  /**
   * @file alloc.h
   * @brief Memory Allocation Interface (Arena & Pool)
   *
   * * 1. Arena (Linear/Stack Allocator):
   * - What: ...
   * - When: ...
   * - Time Complexity: O(1) for alloc, O(1) for reset.
   */
  ```
- Prototypes column-aligned: return types padded to one width,
  names padded so the `(` line up:

  ```c
  Arena* arena_create             (const u64 s_arena, const u64 max_nodes);
  void*  arena_alloc              (Arena* arena, const u64 s_alloc);
  bool   arena_destroy            (Arena* arena);
  ```

## Naming
- Public: `module_verb_object` — `arena_alloc`, `pool_get_size_used`,
  `chess_position_play`.
- Private: double underscore plus module — `__arena_is_full`,
  `__pool_free_region_find`, `__alloc_utils_max`. Small helpers are
  `static inline`.
- Sizes and counts: `s_` prefix (`s_arena`, `s_alloc`, `s_block`),
  maximums `max_` or `m_`.
- Types: `PascalCase` typedefs for structs; macros and constants
  `UPPER_SNAKE`, prefixed by module (`CHESS_HEADER_MASK`).
- Fixed-width typedefs from `utils.h`: `u8 u16 u32 u64 i8 i16 i32
  i64 f32 f64`. Never bare `int`/`long` for data.

## Code
- 2-space indent; `{` on the same line; spaces around every binary
  operator; `Type* name` (star with the type).
- Group same-typed struct fields under one type, one per line:

  ```c
  struct arena {
    u64
      s_arena,
      s_nodes,
      max_nodes;

    void* memory,
        * ptr;

    struct arena* next;
  };
  ```
- Align `=` in runs of assignments.
- Guard clauses first, single-statement bodies without braces:

  ```c
  if (arena == NULL || s_alloc == 0)
    return NULL;
  ```
- Long `for` headers split one clause per line.
- File sections marked with banners:
  `// ====================# PUBLIC #====================` and
  `// ====================# PRIVATE #===================`.

## Design
- Memory: arenas and pools (the user's `alloc` library) instead of
  scattered `malloc`/`free`; free whole lifetimes at once. Every
  allocation checked; every create has a destroy returning `bool`.
- Data: pack state into machine words when it pays — bit fields in
  `u32`/`u16` with documented layouts (a bit diagram in the private
  header), masks and shifts as named macros, `_ALL` masks with every
  bit set. Explain the encoding once, in the header, and keep the
  bit twiddling in one or two functions.
- No undefined behavior: unsigned for bit work (`1U << n`), explicit
  casts on narrowing, bounds checked before indexing.
- Macros that touch arguments parenthesize every use and wrap
  multi-line bodies with `\`.

## Tests
One `test/<lib>/t<lib>.c` per library: `main` runs scoped blocks,
each printing what it tests, using `assert` on every result, and
destroying what it created.

## Blank lines between blocks
- A blank line after every closing `}` of an `if` / `for` / `while` /
  `switch` when more code follows at that level.
- A blank line before a `return` that follows a group of statements;
  none for a guard's single statement.
- A blank line between a function's declarations and its logic.

  ```c
  Arena* arena = (Arena*)malloc(sizeof(struct arena));
  if (arena == NULL)
    return NULL;

  arena->memory = calloc(1, __arena_size_memory(arena));
  if (arena->memory == NULL) {
    free(arena);
    return NULL;
  }

  arena->next = NULL;

  return arena;
  ```

## Before finishing
Build with the makefile under `-Wall -Wextra -Werror`, run the test
programs (and valgrind if available), then re-read the diff for:
unaligned prototypes, missing `__` on internals, unchecked
allocations, bare `int`, unspaced operators.
