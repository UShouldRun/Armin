---
name: cpp-style
description: The user's C++ — written as close to C as possible, low abstraction so the compiler sees everything; header-only libraries with .inl implementations, fixed-width types, templates only for compile-time widths, HLS-friendly code. Load before writing or editing C++.
---
The rule: **C++ is C plus the few features that help.** Use a
feature only when it removes real duplication or gives the compiler
more information; otherwise write the C. Load c-style too — its
naming, layout and spacing rules all apply.

## Features that earn their place
- Templates over compile-time integers, for widths and formats:
  `template<u8 n, u8 i, u8 f> struct lns { uint_t<n> bits; ... };`
  — one definition for lns8/lns16, fully resolved at compile time.
- Operator overloading on numeric value types (`+ - * /`, compound
  assignment, comparisons), so arithmetic code reads like math.
- `explicit` conversion operators; `const` member functions;
  `constexpr` tables and helpers.
- References for non-null in-out parameters.

## Features to avoid
- Virtual functions, inheritance hierarchies, RTTI, exceptions.
- STL containers and algorithms in hot or synthesizable code
  (fixed-size arrays instead); `std::string` and iostreams
  (`cstdio` instead); smart pointers (own memory explicitly, arenas).
- Heavy template metaprogramming, lambdas capturing state, `auto`
  where the type matters to the reader.
- Anything that hides allocation or control flow from the compiler.

## Layout
- Header-only libraries: declarations in `name.hpp` with a column-
  aligned member list, implementations in `name.inl` included at the
  bottom; guards `#ifndef __NAME_H__` … `#endif // !__NAME_H__`.
- C headers as `<cstdio>`, `<cstdint>`, `<cmath>`; the user's fixed-
  width typedefs (`u8 … u64`, `i8 … i64`, `f32`, `f64`).
- Aligned member declarations:

  ```cpp
  lns operator+   (const lns other)   const;
  lns operator*   (const lns other)   const;
  lns square      ()                  const;
  bool operator== (const lns other)   const;
  ```
- Value types are plain structs holding raw bits; pass small ones by
  value.

## HLS (Vitis HLS) code
- Synthesizable functions: fixed-size arrays, static bounds on every
  loop, no recursion, no dynamic allocation, no pointers to pointers.
- `ap_uint<N>` / `ap_int<N>` (or the project's `uint_t<n>`) for exact
  widths; lookup tables as `const` arrays the tool can map to ROM.
- Pragmas next to what they affect (`#pragma HLS PIPELINE II=1`,
  `UNROLL`, `ARRAY_PARTITION`) with a comment saying why.
- Keep a software-simulation path (e.g. `lnssim.hpp`) next to the
  hardware one, and test both against float32 references.

## Blank lines between blocks
Same as c-style: a blank line after each closing `}` when code
follows, before a `return` that ends a group of statements, and
between declarations and logic.

## Before finishing
Build with warnings as errors, run the tests or emulation against
reference values, and check the diff for any feature from the avoid
list.
