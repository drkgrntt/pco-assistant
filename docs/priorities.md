This is a scratch pad file for Derek to write down thoughts on priorities, but as always, I'm open to pushback.

- Immediate
    - Evaluate issues.md
        - Hit smaller items - **done 2026-09-12**. See `docs/issues.md`, every item is now annotated with what happened to it. The genuinely quick wins (Team tab width, filter/label alignment and separation, browse-row song flags, multi-tab poll visibility, accordion affordance, landing copy) are shipped. One more (the Team tab's undercounted "Warnings: N" chip) turned out to be a real bug hiding in the list, not a request, and got fixed alongside the rest since it was small and well-understood.
        - Plan more involved items - **done 2026-09-12**. Three items were bigger than a quick win and got their own design docs instead of a rushed inline fix:
            - `docs/plans/not-started/mobile-responsive-build-page-plan.md` (issues.md's mobile styles item)
            - `docs/plans/not-started/pco-rate-limit-user-visibility-plan.md` (the two 429-handling items)
            - `docs/plans/not-started/max-consecutive-weeks-preference-plan.md` (the diva-preferences item)

- Short term / smaller sized
    - dismissable warnings on the build page (already has its own plan doc, from before this pass: `docs/plans/not-started/dismissable-warnings-plan.md`)
    - song and person detail pages (already has its own plan doc, from before this pass: `docs/plans/not-started/song-person-detail-pages-plan.md`)
    - vocal range crowdsourcing (already has its own plan doc, from before this pass: `docs/plans/not-started/vocal-range-matrix-and-crowdsourced-default-plan.md`)

- Longer term / larger sized
    - admin tooling

- Corrections from this pass (2026-09-12)
    - The "publish vs sync" item wasn't actually about Merge/"Pull in changes" - it was specifically Publish vs. the "Re-sync key, length & arrangement" button. They're not redundant (Publish is the full local-to-PCO sync; the other button is a narrow PATCH-only fix for stale info on already-published songs, a gap Publish never covers on its own), so didn't combine them - the button's *name* was the real source of confusion, since "sync" already means something else on this page (the poll banner and Merge both pull FROM PCO; this button only ever pushes TO PCO). Renamed it to "Re-push key, length & arrangement" instead. Full reasoning in `docs/issues.md`'s corresponding item.
    - The arrangements item didn't need new investigation - `docs/ideas.md` already has a writeup of the real gap (one cached arrangement per song org-wide, no per-occurrence override), so pointed at that instead of re-deriving it.
