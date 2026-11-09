# Design canvas source

Source of the [Duolify App Design canvas](https://claude.ai/artifact/FggicArBXWkRVSfQnk7Ukn): every screen and state, desktop first and then mobile, plus foundations, components and user flows. `canvas.json` is the board index (pages, positions, sizes); each `*.dc.html` file is one artboard.

These files are design references, not app code. They load a `support.js` runtime that only the canvas provides, so open the canvas link to view them rather than opening the files directly. Token values match [tokens.json](../tokens.json); when the two differ, `tokens.json` wins.

| Board | Covers |
| --- | --- |
| `Main` | Screen and state inventory, theme decision, open questions |
| `Foundations`, `Components` | Color, type, icons; shared components C1–C17 and their states |
| `Flows` | Use cases UC1–UC16 traced across screen IDs |
| `S01`–`S24` | Product screens (onboarding chat, create flow, learn loop, social, credits, AI tools, settings) |
| `E15-*`, `E16-*` | Sharing and marketplace; course steering |
| `D1`–`D3` | Responsive pages: learn shell, player, moderator console |
