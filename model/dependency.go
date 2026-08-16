package model

type Dependency struct {
	ItemID      string `json:"item_id"`
	DependsOnID string `json:"depends_on_id"`
}

// Membership is one item's place in one project, carrying its order within
// that project. An item belongs to as many projects as it is filed under.
type Membership struct {
	ItemID    string `json:"item_id"`
	ProjectID string `json:"project_id"`
	Position  int    `json:"position"`
}
