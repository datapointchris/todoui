package graph

// WouldCycle returns true if adding an edge from `from` to `to` in the given
// adjacency list would create a cycle. Uses DFS from `to` to check if `from`
// is reachable.
func WouldCycle(adj map[int64][]int64, from, to int64) bool {
	visited := make(map[int64]bool)
	return dfs(adj, to, from, visited)
}

func dfs(adj map[int64][]int64, current, target int64, visited map[int64]bool) bool {
	if current == target {
		return true
	}
	if visited[current] {
		return false
	}
	visited[current] = true
	for _, neighbor := range adj[current] {
		if dfs(adj, neighbor, target, visited) {
			return true
		}
	}
	return false
}
