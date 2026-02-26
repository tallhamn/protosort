# Proto File Ordering Style Guide

## File Header

Put these at the top, always in this order:

1. License or copyright comment
2. `syntax`
3. `package`
4. `option` statements, sorted alphabetically
5. `import` statements, sorted alphabetically

## Body

**If the file declares a service**, start with it. It acts as a table of contents — a reader should see what the file *does* before seeing the types it uses. Request and response messages come next, paired up and in the same order as their RPCs. If `GetTrip` is the first RPC, then `GetTripRequest` and `GetTripResponse` come first.

Optionally, RPCs within a service can be sorted with `--sort-rpcs alpha` (alphabetical) or `--sort-rpcs grouped` (group by resource name, then alphabetical within each group — e.g., all `*Trip` RPCs together, then all `*User` RPCs).

**After that (or first, if there's no service), arrange the remaining types:**

- If a type is used by more than one other type in the file, it's a **core type**. Core types are sorted alphabetically.
- If a type is used by exactly one other type in the file, put it **immediately before** that type. If there's a chain (`A` uses `B` uses `C`, all single-use), stack them bottom-up: `C`, then `B`, then `A`.
- If a type isn't used by anything else in the file, it goes **at the bottom**, sorted alphabetically.

## Comments

Comments belong to the declaration they describe. When a declaration moves, its comments move with it.

- **Comments above a declaration** (with no blank line between) belong to that declaration.
- **Comments inside a declaration** (between `{` and `}`) are part of it and never move independently.
- **A comment block separated by a blank line** from the next declaration is still attached to it — it's treated as a preamble.
- **Inline comments** on the same line as a declaration's opening are part of it.
- **Freestanding section dividers** (e.g. `// === Messages ===`) are dropped, since the tool imposes its own ordering and they'd end up in the wrong place.

In practice, if you write your comments directly above or inside your declarations (which is the normal convention), they'll always stay with the right thing.

## Whitespace

The tool normalizes whitespace **between** declarations but does not touch whitespace **inside** them.

- One blank line between each top-level declaration.
- One blank line between sections (e.g., between the last request/response pair and the first core type).
- Whatever whitespace exists inside a message, enum, or service body is preserved exactly.

## In Short

Read the file top to bottom and you should encounter things in this order:

1. The service and its request/response pairs (if any)
2. Core types used across multiple declarations
3. Helper types, each right before the thing that needs it
4. Unreferenced types last

Comments travel with their declaration. One blank line between everything. Nothing inside a declaration is touched.
