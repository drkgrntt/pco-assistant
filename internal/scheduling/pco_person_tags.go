package scheduling

// A thin passthrough to live PCO data, no local table - the person-scoped
// counterpart to the Song feature's own PCO tag surfacing. Unlike
// FetchActiveSongs' org-wide song library, there's no bounded, cacheable
// "list every tag" call here worth memoizing: ListPCOPersonTags is called
// once per relevant person (assigned roster members, or one clicked
// candidate), each call already cheap and already bounded by its own call
// site - see rosters_builder.go/rosters_candidate_warnings.go for why an
// eager per-candidate-pool fetch is deliberately not done here.

import (
	"context"

	pco "github.com/drkgrntt/go-pco"
)

// ListPCOPersonTags lists personID's currently-assigned PCO tags across all
// of its tag groups (person-scoped ones like "Instruments Played"). Not
// organization-scoped - PCO's tag data is looked up by person id alone, same
// call shape as GetPersonTags itself.
func (s *Store) ListPCOPersonTags(ctx context.Context, personID string) ([]PCOTag, error) {
	resp, err := pco.GetPersonTags(ctx, personID)
	if err != nil {
		return nil, err
	}

	tags := make([]PCOTag, 0, len(resp.Data))
	for _, t := range resp.Data {
		var groupID string
		if t.Relationships.TagGroup.Data != nil {
			groupID = t.Relationships.TagGroup.Data.ID
		}
		tags = append(tags, PCOTag{
			ID:      t.ID,
			GroupID: groupID,
			Name:    t.Attributes.Name,
		})
	}

	return tags, nil
}
