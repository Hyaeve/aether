# Aether Design System

The supplied Aura and LitePan screenshots are the layout reference. User-selected colors override generic skill recommendations. Automated design search returned a mismatched marketing/education direction, so it was not adopted.

- Primary `#3A4980`, soft lavender `#B4B9D6`, sidebar accent `#5263A8`.
- Sidebar `#181E2D`; white content surfaces with neutral gray background.
- Secondary functional colors: muted green (connected), amber (pending), red (failure).
- Fonts: Inter/Segoe UI for Latin; Microsoft YaHei/PingFang SC for Chinese. System fonts only, no remote font dependency.
- Alternate font combinations: Segoe UI + Microsoft YaHei; system-ui + PingFang SC.
- Typography: 10/11/12/14/17/23/28px, zero tracking; no viewport-scaled type.
- Spacing: 4/8/12/16/20/24/32/48px. Repeated item cards <= 8px radius.
- Fixed sidebar, fixed breadcrumb/traffic header, active page tab, unframed page sections.
- Theme: light, dark, system; persists locally and reacts to OS theme changes.
- Accessibility: labeled icon controls, visible focus, modal focus containment and restoration, Escape close, live notifications, reduced motion.
- States: genuine empty state, pending connection, disabled, error, active task, interrupted task. No fabricated storage or task metrics.
- Login: 3:2 desktop split, deterministic canvas starfield and faint constellations, three floating elliptical orbits. Provider logos remain upright and complete a revolution in 180 seconds; motion pauses on request, in hidden tabs, and with reduced-motion preferences. Mobile hides the decorative side. Brand assets are bundled locally; sources are recorded in `web/public/providers/SOURCES.md`.
