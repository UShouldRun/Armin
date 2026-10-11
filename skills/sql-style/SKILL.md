---
name: sql-style
description: The user's SQL style — all lowercase, aligned column types, in_/v_ prefixes, one clause per line, and lean id-based queries that touch as little data as possible. Load before writing procedures, migrations or queries.
---
## Case
- Everything lowercase: keywords, built-in functions, types —
  `create procedure`, `select`, `uuid_to_bin`, `varchar(255)`. This
  includes SQL inside application strings (`"call fetch_event(?)"`)
  and SQL built in strings for `prepare`.
- Exceptions: case-sensitive identifiers (e.g. a database name), string
  literals that are data (`'INSERT'`), and comments.

## Layout
- 2-space indentation.
- One column or parameter per line, with the types aligned:

  ```sql
  create procedure create_event(
    in in_name                 varchar(127),
    in in_event_date           date,
    in in_type_id              tinyint unsigned
  )
  ```
- Parameters prefixed `in_`, locals `v_`. Procedure names are
  snake_case verbs: `fetch_event_by_id`, `exists_event_name`,
  `update_member`.
- `drop procedure if exists` lines grouped at the top by verb, then
  `delimiter //`.
- `insert` lists one column per line, `) values (` on its own line.
- `select`, `from`, `where`, `group by`, `order by`, `limit` each on
  their own line.

## Types
- The smallest type that fits the domain: `tinyint unsigned`,
  `smallint unsigned`, `int unsigned` for ids and codes;
  `varchar(127)` / `varchar(255)`; uuids as `binary(16)`, not text.
- In a migration, an altered table takes the types the newer create
  files use. If no create file shows the column's type, ask before
  choosing one.

## Lean queries
The schema's compact types exist to make queries fast and small.
- Filter and join on typed ids (`d.status_id = 2`), never on joined
  name strings.
- Page / limit before per-row work (JSON building, subqueries), so
  that work runs only on the returned rows.
- One grouped join beats repeated correlated subqueries; a keyed
  `left join` beats materializing a union or a whole table.
- Check every query: are the ids used, does it hit an index (PK
  prefix), does it avoid temp and derived tables it doesn't need?

## Blank lines between blocks
- A blank line between statements inside a procedure (each
  `declare` group, `select`, `insert`, `update` is its own block).
- A blank line after each `end if;` / `end loop;` when more
  statements follow.
- Two blank lines between procedures.

## Before finishing
Re-read the diff for missing blank lines between blocks, uppercase keywords, unaligned type columns,
missing `in_`/`v_` prefixes, and name-based filters.
