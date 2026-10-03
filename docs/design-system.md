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
- Tools use plugin cards with honest unfinished states. About reuses the animated starfield and checks GitHub releases explicitly.
- Accessibility: labeled icon controls, visible focus, modal focus containment and restoration, Escape close, live notifications, reduced motion.
- States: genuine empty state, pending connection, disabled, error, active task, interrupted task. No fabricated storage or task metrics.
- Login: 3:2 desktop split, twinkling canvas stars, a cached particulate galaxy with dark dust lanes, faint constellations and occasional meteor trails; three floating elliptical orbits. Provider logos have no added frames, remain upright and complete a revolution in 180 seconds. Motion runs automatically without a playback button; hidden tabs and reduced-motion preferences still suspend animation. Mobile hides the decorative side. Brand assets are bundled locally; sources are recorded in `web/public/providers/SOURCES.md`.
- Login input focus stays inside each existing rounded border; password wrapper highlights as one control. A closed eye indicates concealed text, an open eye indicates visible text; accessible labels describe the next action.
