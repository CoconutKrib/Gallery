package recognition

// FaceEmbedding bundles a face's DB identity with its float32 embedding vector.
// Used by both the suggestion and clustering pipelines.
type FaceEmbedding struct {
	FaceID    int64
	PersonID  *int64 // nil when unidentified
	Embedding []float32
}

// Suggestion is a candidate identity assignment for an unidentified face.
type Suggestion struct {
	FaceID   int64
	PersonID int64
	Score    float32 // cosine similarity (0–1)
}

// Suggest assigns candidate person_ids to unidentified faces by comparing each
// face embedding against the per-person mean embedding.
//
// verified contains all verified faces with non-nil PersonID and non-nil
// embedding (used to build per-person means). unidentified contains faces with
// nil PersonID and non-nil embedding. threshold is the cosine distance cutoff —
// a suggestion is made when similarity >= (1 - threshold).
func Suggest(unidentified, verified []FaceEmbedding, threshold float64) []Suggestion {
	if len(unidentified) == 0 || len(verified) == 0 {
		return nil
	}

	// Group verified face embeddings by person.
	byPerson := make(map[int64][][]float32)
	for _, fe := range verified {
		if fe.PersonID == nil || len(fe.Embedding) == 0 {
			continue
		}
		pid := *fe.PersonID
		byPerson[pid] = append(byPerson[pid], fe.Embedding)
	}
	if len(byPerson) == 0 {
		return nil
	}

	// Compute L2-normalised mean embedding per person.
	personMeans := make(map[int64][]float32, len(byPerson))
	for pid, embs := range byPerson {
		mean := make([]float32, embDim)
		for _, e := range embs {
			for i, v := range e {
				if i < len(mean) {
					mean[i] += v
				}
			}
		}
		l2NormalizeInPlace(mean)
		personMeans[pid] = mean
	}

	simThreshold := float32(1.0 - threshold)

	var suggestions []Suggestion
	for _, face := range unidentified {
		if len(face.Embedding) == 0 {
			continue
		}
		var bestPerson int64
		var bestSim float32 = -1
		for pid, mean := range personMeans {
			sim := dotProduct(face.Embedding, mean)
			if sim > bestSim {
				bestSim = sim
				bestPerson = pid
			}
		}
		if bestSim >= simThreshold {
			suggestions = append(suggestions, Suggestion{
				FaceID:   face.FaceID,
				PersonID: bestPerson,
				Score:    bestSim,
			})
		}
	}
	return suggestions
}

// dotProduct returns the dot product of two float32 slices.
// Embeddings from ArcFace are L2-normalised, so dot product == cosine similarity.
func dotProduct(a, b []float32) float32 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var sum float32
	for i := 0; i < n; i++ {
		sum += a[i] * b[i]
	}
	return sum
}
