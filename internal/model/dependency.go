package model

type Dependency struct {
	ItemID      int64 `json:"item_id"`
	DependsOnID int64 `json:"depends_on_id"`
}
