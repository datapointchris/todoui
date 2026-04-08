package graph

// WouldCycle returns true if adding an edge from `from` to `to` in the given
// adjacency list would create a cycle. Uses DFS from `to` to check if `from`
// is reachable.
func WouldCycle(adj map[string][]string, from, to string) bool {
	visited := make(map[string]bool)
	return dfs(adj, to, from, visited)
}

func dfs(adj map[string][]string, current, target string, visited map[string]bool) bool {
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
