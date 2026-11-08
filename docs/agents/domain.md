# Domain docs

How agent skills consume Duolify's domain documentation. Layout: single-context.

## Before exploring, read these

- `CONTEXT.md` at the repo root: the glossary. (Skills that mention `GLOSSARY.md` mean this file.)
- `docs/adr/`: read the ADRs that touch the area you are about to change.
- `openspec/specs/<capability>/spec.md`: the current requirements for the capability.
- `docs/epics/README.md`: epic order and dependencies.

## Use the glossary's vocabulary

Name domain concepts exactly as `CONTEXT.md` defines them, in issue titles, test names, code and UI copy. A missing term is a signal: either the language is invented (reconsider) or there is a real gap (add it to `CONTEXT.md` in the same PR).

## Flag ADR conflicts

If your output contradicts an ADR, say so explicitly and propose a new ADR instead of overriding silently:

> _Contradicts ADR 0007 (credits ledger), but worth reopening because…_
