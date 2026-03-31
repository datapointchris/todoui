package model

type Dependency struct {
	ItemID      string `json:"item_id"`
	DependsOnID string `json:"depends_on_id"`
}
