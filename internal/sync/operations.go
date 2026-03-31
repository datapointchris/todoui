package sync

// OpType identifies the kind of sync operation queued in pending_sync.
type OpType string

const (
	OpCreateProject     OpType = "create_project"
	OpUpdateProject     OpType = "update_project"
	OpDeleteProject     OpType = "delete_project"
	OpCreateItem        OpType = "create_item"
	OpUpdateItem        OpType = "update_item"
	OpDeleteItem        OpType = "delete_item"
	OpReorderItem       OpType = "reorder_item"
	OpAddToProject      OpType = "add_to_project"
	OpRemoveFromProject OpType = "remove_from_project"
	OpCreateTask        OpType = "create_task"
	OpUpdateTask        OpType = "update_task"
	OpDeleteTask        OpType = "delete_task"
	OpCompleteTask      OpType = "complete_task"
	OpAddDependency     OpType = "add_dependency"
	OpRemoveDependency  OpType = "remove_dependency"
)

// entityType returns the entity category for the pending_sync table.
func (o OpType) entityType() string {
	switch o {
	case OpCreateProject, OpUpdateProject, OpDeleteProject:
		return "project"
	case OpCreateItem, OpUpdateItem, OpDeleteItem, OpReorderItem:
		return "item"
	case OpAddToProject, OpRemoveFromProject:
		return "membership"
	case OpCreateTask, OpUpdateTask, OpDeleteTask, OpCompleteTask:
		return "task"
	case OpAddDependency, OpRemoveDependency:
		return "dependency"
	default:
		return "unknown"
	}
}
