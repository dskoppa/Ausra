# Aušra visual system

Aušra uses a dawn-inspired command-center style. Deep slate keeps operational data calm; sunrise amber draws attention to context; teal identifies the primary action and healthy connections. The interface has dark and light themes using the same semantic tokens in `web/style.css`. Dark is the default, and the user's choice is stored locally in the browser.

| Semantic token | Dark | Light | Use |
| --- | --- | --- | --- |
| `--bg` | `#08141c` | `#f2f7f6` | App background |
| `--surface` | `#10212b` | `#ffffff` | Panels and cards |
| `--surface-raised` | `#182d37` | `#f0f7f5` | Raised controls and panels |
| `--text` | `#ecf5f3` | `#152c31` | Primary text |
| `--muted` | `#9cb0b4` | `#526e73` | Secondary text |
| `--border` | `#29434d` | `#d8e6e3` | Dividers and card edges |
| `--accent` | `#72dbc8` | `#186f65` | Primary action and active navigation |
| `--sunrise` | `#f3b571` | `#a65e23` | Brand and attention cues |
| `--success` | `#78dcb3` | `#22734c` | Successful run or healthy connection |
| `--warning` | `#f4c979` | `#805613` | Pending approval or caution |
| `--danger` | `#f3948e` | `#a73f3b` | Failed run or destructive action |
| `--info` | `#9abcf4` | `#305e9a` | Running or queued state |

Status colors always appear with text labels. Decorative orbit graphics carry no operational meaning. Running, approval, failure, and success states must remain understandable if gradients, animation, or color are unavailable.

The layout uses a 1240 px content width, 16 px cards, and 10 px controls. The spacing scale is 4, 8, 12, 16, 24, and 32 px; spacious layouts can use wider gaps to separate major sections. The hero uses a two-column layout at desktop widths. At 1000 px the hero and brand tighten; below 760 px forms and lists stack and summary cards become two columns; below 420 px button labels and form padding tighten. Operator data uses 11 px captions, body copy uses 14 px, section headings use 23 px, and IDs, commands, and logs use a monospace stack. The actual CSS tokens are in `web/style.css`.

Primary, ghost, and destructive controls have distinct default, hover, focus, active, and disabled states. Icons accompany a text label for operational actions and status; decorative orbit symbols are hidden from assistive technology. Run and schedule badges pair a written status with a distinct symbol and outline shape, so color is never the only cue. The header density control switches between spacious and compact layouts and stores the choice in browser storage, like the theme control. Display preferences also save the selected IANA time zone, favorite projects, and the overview section order and visibility in this browser. The browser time zone is the default.

Overview cards open their matching collection; the active-run and approval pulse counts open Runs with an exact status filter. The Runs view announces status or step changes through one polite live region, summarizes bursts of more than two changes, and lets operators pause polling while keeping manual refresh available. These small controls stay in the prototype's existing DOM code; the planned React migration will move them to shared components rather than adding a separate framework now.

Runs can be viewed as detailed cards or a 50-row table. The table uses the same status filter as the cards, provides a direct link to each run, and reduces cell padding in compact density. A horizontal scroll area keeps columns readable on narrow screens.

Transitions are short and tied to state changes: context panels enter in 200 ms, a changed run highlights for 650 ms, and the current workflow step pulses while running. Status text and the step list remain understandable without animation. The `prefers-reduced-motion` media query reduces these animations to near zero. Focus indicators remain visible in both themes. When adding new screens, reuse semantic tokens rather than inserting literal colors, and verify keyboard focus, contrast, 200% zoom, and narrow-screen behavior.

The current UI is a vanilla JavaScript prototype. The planned component system uses React and TypeScript, Radix Primitives for accessible interactions, TanStack Query for API state, and React Flow for the workflow editor. Aušra's CSS tokens and layouts remain the visual source of truth. See `LIBRARIES.md` for adoption and verification checks.
