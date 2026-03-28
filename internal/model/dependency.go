package model

type Dependency struct {
	TodoID      int64 `json:"todo_id"`
	DependsOnID int64 `json:"depends_on_id"`
}
