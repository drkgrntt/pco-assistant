This is a scratch pad file for Derek to write down thoughts on priorities, but as always, I'm open to pushback.

- Immediate
    - Evaluate issues.md
        - Hit smaller items - **done 2026-09-12**. See `docs/issues.md`, every item is now annotated with what happened to it. The genuinely quick wins (Team tab width, filter/label alignment and separation, browse-row song flags, multi-tab poll visibility, accordion affordance, landing copy) are shipped. One more (the Team tab's undercounted "Warnings: N" chip) turned out to be a real bug hiding in the list, not a request, and got fixed alongside the rest since it was small and well-understood.
        - Plan more involved items - **done 2026-09-12**. Three items were bigger than a quick win and got their own design docs instead of a rushed inline fix:
            - `docs/plans/shipped/mobile-responsiveness-plan.md` (issues.md's mobile styles item) - **shipped 2026-09-14**, broadened to app-wide (setup pages included) per direct instruction
            - `docs/plans/shipped/pco-rate-limit-user-visibility-plan.md` (the two 429-handling items) - Phase 1 (the page-level "rate limit reached" banner) **shipped 2026-09-14**; Phase 2 as originally scoped there (per-row marking/auto-retry) is still open, but a smaller fragment-level extension (the Build page Songs tab's `SyncCheck` poll now surfaces/clears the same banner) **shipped 2026-09-15**, doc moved to `shipped`
            - `docs/plans/not-started/max-consecutive-weeks-preference-plan.md` (the diva-preferences item)

- Short term / smaller sized
    - dismissable warnings on the build page (already has its own plan doc, from before this pass: `docs/plans/shipped/dismissable-warnings-plan.md`) - **done 2026-09-20**. Songs tab only (avoid-pairing/recently-repeated/vocal-range-mismatch), Team tab deliberately left out of scope - see that doc's own Status line.
    - song and person detail pages (already has its own plan doc, from before this pass: `docs/plans/in-progress/song-person-detail-pages-plan.md`) - Phases 1-2 (song + people library/detail pages) **built 2026-09-21**, on branch `song-person-detail-pages`, not yet merged. The plan's original Phase 3 (retiring the 8 existing flat setup pages) was reconsidered and dropped - the flat pages stay permanently as a batch-entry workflow alongside the new per-entity pages; see the plan doc's "Disposition" section for why.
    - vocal range crowdsourcing (already has its own plan doc, from before this pass: `docs/plans/not-started/vocal-range-matrix-and-crowdsourced-default-plan.md`)
    - security/prod-readiness follow-ups from the 2026-09-14 pre-launch
      audit - medium/low-priority items only, the high-priority one is
      listed below; see `docs/plans/not-started/security-hardening-followups-plan.md`

- Longer term / larger sized
    - admin tooling
    - encrypting PCO access/refresh tokens at rest (the one High-priority
      item in `docs/plans/not-started/security-hardening-followups-plan.md`)
      - **done 2026-09-15**. AES-256-GCM at the app layer, key from an
      optional `SESSION_TOKEN_ENCRYPTION_KEY` env var, self-upgrading
      (no backfill migration) - see that plan doc's own "High" item for
      the full detail.
