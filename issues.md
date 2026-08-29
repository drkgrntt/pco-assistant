- Fixed. Every song row's arrangement-info line now always reserves the
  same height, whether or not that song actually has cached info to show -
  a song without it used to make its row visibly shorter than its
  neighbors.
- Fixed - and it was a real bug, not the "first spot" memory being
  overeager. Dragging the *last* song (nothing below it to drop onto)
  could get silently misread as "drop at the very top of service," which
  then legitimately (if wrongly) became the new remembered first-song
  anchor. The drag code's fallback for "pointer never left the dragged
  row's own space" defaulted to nothing, which the drop logic then treated
  as top-of-service instead of "no change." Fixed at the source so the
  first-spot memory itself didn't need touching.
- Also real: publishing a song only ever linked the bare song, not its
  arrangement - confirmed live, an item created that way shows no chord
  chart, lyrics, or key in Planning Center at all, unlike a song added by
  hand there. Added arrangement + key linking (plus the item's length and
  a starter description from the arrangement's own notes) to `go-pco` and
  wired it into publish, verified live end to end.
