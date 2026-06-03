package recognition

// FaceCluster groups face IDs that the algorithm believes belong to the same person.
type FaceCluster struct {
	ClusterID int
	FaceIDs   []int64
}

// Cluster groups unidentified face embeddings using a union-find approach
// (effectively single-linkage clustering).
//
// threshold is the cosine distance cutoff (from config): faces with cosine
// distance < threshold (equivalently, similarity >= 1-threshold) are merged
// into the same cluster. minSamples is the minimum cluster size — smaller
// clusters are discarded from the result.
func Cluster(faces []FaceEmbedding, threshold float64, minSamples int) []FaceCluster {
	n := len(faces)
	if n == 0 {
		return nil
	}
	if minSamples < 2 {
		minSamples = 2
	}

	// Union-Find with path compression.
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}

	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}

	union := func(i, j int) {
		pi, pj := find(i), find(j)
		if pi != pj {
			parent[pi] = pj
		}
	}

	simThreshold := float32(1.0 - threshold)

	for i := 0; i < n; i++ {
		if len(faces[i].Embedding) == 0 {
			continue
		}
		for j := i + 1; j < n; j++ {
			if len(faces[j].Embedding) == 0 {
				continue
			}
			if dotProduct(faces[i].Embedding, faces[j].Embedding) >= simThreshold {
				union(i, j)
			}
		}
	}

	// Collect connected components.
	groups := make(map[int][]int64)
	for i, fe := range faces {
		root := find(i)
		groups[root] = append(groups[root], fe.FaceID)
	}

	var clusters []FaceCluster
	clusterID := 0
	for _, faceIDs := range groups {
		if len(faceIDs) >= minSamples {
			clusters = append(clusters, FaceCluster{
				ClusterID: clusterID,
				FaceIDs:   faceIDs,
			})
			clusterID++
		}
	}
	return clusters
}
