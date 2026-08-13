# Kindlast: Claude Code instructions

The instructions live in [`AGENTS.md`](./AGENTS.md), so every agent reads the
same rules rather than each tool carrying its own drifting copy. Claude Code
reads `CLAUDE.md` and not `AGENTS.md`, so this file imports it:

@AGENTS.md

Anything Claude Code specific belongs below this line. Everything that applies
to any agent belongs in `AGENTS.md`.
