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

- File services consolidate file browsing, WebDAV and local mounts into three page tabs, with browsing first and legacy-route redirects.
- Center emblems have a soft elliptical light beneath them without a mesh. Meteors start across the top band from one-third of the scene width to the right edge, with tails spanning 4%–6.5% of travel.
- Login fields and button share a 350px maximum width. Account fields and session duration share a 40px height and equal width. Tab glows use a rounded pseudo-element independent of the straight active underline; WebDAV's cloud outline is enlarged to match adjacent icons.

- Select triggers use 12px corners while preserving native keyboard and mobile picker behavior. Breadcrumb controls, refresh and log-mode buttons keep accessible labels without hover titles.
- Unselected navigation and page tabs have a softer rounded hover wash between their resting and active states. Framed orbit-provider icons use 25% corner radii, matching the Quark mark.
- Login and About center emblems share a responsive Canvas gravity well: a depressed membrane mesh with a dark interior, drawn on resize without an additional animation loop.

- Login and account settings share equal-height 62px rounded icon fields with accessible names and placeholder text, without visible field labels. Account settings are a compact 380px panel. Login uses larger consistent typography and no version footer.
- About keeps provider icons on a slow elliptical orbit around the central identity, with update and repository buttons at the bottom. WebDAV combines a cloud outline with DAV text; local mount/storage uses CloudDownload.
- Selected and unselected tabs use identical 17px/600 typography to prevent layout shifts. Rounded diffused selection remains visible. Deep cosmic blue-purple navigation includes animated twinkling selection stars, respecting reduced motion.
- Meteor tails span only 1.8%–3.8% of travel. Raw JSON logs remain fully wrapped with no line clamp, using 14px text and 24px leading.

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
