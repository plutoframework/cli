# Working on Pluto CLI

Only modify this repository. Sibling framework/example repos are reference material.
Use Cobra, stdlib first, explicit dependencies, no magic registration. The module is
github.com/plutoframework/cli. Commit/push only with task authorization.

Keep code and generated templates readable, expanded, and <=1,000 lines per file.
The formatter target is 80 columns; do not enforce literal/comment byte lengths.
Run ./scripts/check.sh and go test -race ./... after generator changes. Integration
tests execute Cobra commands and compile generated applications. Run the bounded
mutation smoke when changing validation/security. Never install hooks in this
checkout as part of tests. Never execute existing user hooks while scaffolding.

Templates are embedded, so edits require rebuilding pluto. Generated app-local
primitives are provisional, not a published Pluto framework API. Keep endpoint
metadata in pluto.toml and derive paths, feature declarations, registries and
OpenAPI. Authored operation/model/handler files must never be overwritten.

Interactive forms are a presentation layer over fresh Cobra dispatch. Keep raw
commands deterministic; auto-prompt only on terminal input AND output, outside CI.
Never open hidden /dev/tty prompts or write before wizard confirmation. Reuse
project validators and saved capabilities. Pass argument slices, never a shell.
Keep Bubble Tea and terminal dependencies confined to the CLI module.


Across languages, put one blank line before a standalone comment block unless
it starts on line 1. Keep consecutive comment lines together and keep the comment
attached to the code below it. Preserve trailing inline comments and any placement
required by compiler/tool directives; never separate a next-line directive from
its target. This preference also applies at the beginning of a code block.
