# Aether Design System

LitePan is the primary layout and interaction reference, including its local AdminView, AddAccountDialog, DriverPickerStep and authorization flow. Aura is no longer the layout reference. User-selected colors remain unchanged.

- Primary `#3A4980`, soft lavender `#B4B9D6`, sidebar accent `#5263A8`.
- Sidebar `#181E2D`; white content surfaces with neutral gray background.
- Secondary functional colors: muted green (connected), amber (pending), red (failure).
- Fonts: Inter/Segoe UI for Latin; Microsoft YaHei/PingFang SC for Chinese. System fonts only, no remote font dependency.
- Alternate font combinations: Segoe UI + Microsoft YaHei; system-ui + PingFang SC.
- Typography: 16px primary navigation, 17px account menu, 14-15px controls, 12px supporting text and 24px page headings; zero tracking and no viewport-scaled type.
- Spacing: 4/8/12/16/20/24/32/48px. Repeated item cards <= 8px radius.
- Fixed 252px sidebar with eleven ungrouped destinations and fixed breadcrumb/traffic header. Task kinds live in page tabs; system settings use inner navigation. No workspace switch or sidebar connection/version footer.
- Theme: one icon cycles light, dark and system; persists locally and reacts to OS theme changes. A task notification bell follows it, with per-account local read markers.
- Number controls share one accessory slot for units and hover/focus steppers. Form focus stays within the existing rounded boundary.
- Storage: cards without a connector footer; compact two-step modal, icon/name-only selection, previous/save controls and lower-left authorization. Small screens retain scrolling without visible scrollbars.
- Tools use four-column plugin cards on desktop with honest unfinished states. About reuses the animated starfield and checks GHCR image revisions explicitly.

## Compact Workspace

- Use a 236px cosmic indigo sidebar, 48px topbar, rounded sidebar right corners and square main-surface junctions. Logs and settings are bottom-pinned. Active navigation has translucent highlights and restrained star points.
- Do not repeat page titles beneath breadcrumbs. Align task tabs and actions in one desktop row. Settings use horizontal tabs without a general-settings section.
- Storage cards use compact padding and 14px corners; the dashed tile is the only add entry and provider icons toggle enabled state. Account menus use compact 14px typography.
- Task forms use two-column rows and select controls for generation mode. A combined source picker places accounts on the left and directory rows on the right; CAS filters eligible providers.
- Meteors retain parallel trajectories at slope 0.30 with short tails. Each trail randomly uses the original duration, twice that duration or three times that duration; fades begin after at least one third of travel. Login fits the viewport without visible scrollbars.
- About displays seven floating provider icons around the central content, using CloudDownload for WebDAV and FolderSync for local storage, matching navigation. Sidebar and tab selection use rounded diffused highlights, not rectangular fills.
- Toasts slide in from the upper right with a colored left edge and filled status icon, then fade and slide out; reduced-motion preferences are respected. Raw logs use wrapped JSON, severity coloring and measured variable-height virtualization, with a single current-mode icon toggle.
- Page tabs use 17px text and a stronger active underline. Workspace scrollbars are thin pale indigo with a darker hover. Logs use a fixed-height virtual list; account settings are framed and About fills remaining page height.
- Accessibility: labeled icon controls, visible focus, modal focus containment and restoration, Escape close, live notifications, reduced motion.
- States: genuine empty state, pending connection, disabled, error, active task, interrupted task. No fabricated storage or task metrics.
- Login: 3:2 desktop split, twinkling canvas stars, a cached particulate galaxy with dark dust lanes, faint constellations and occasional meteor trails; three floating elliptical orbits. Provider logos have no added frames, remain upright and complete a revolution in 180 seconds. Motion runs automatically without a playback button; hidden tabs and reduced-motion preferences still suspend animation. Mobile hides the decorative side. Brand assets are bundled locally; sources are recorded in `web/public/providers/SOURCES.md`.
- Login input focus stays inside each existing rounded border; password wrapper highlights as one control. A closed eye indicates concealed text, an open eye indicates visible text; accessible labels describe the next action.
