package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"

	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/protobuf/proto"
)

func main() {
	result := []*qdrant.QueryPoints{}
	dim := 64
	for _ = range 100 {
		vec := make([]float32, dim)
		for i := range dim {
			vec[i] = rand.Float32()
		}
		newPoint := &qdrant.QueryPoints{
			CollectionName: "my_collection",
			Query:          qdrant.NewQuery(vec...),
			Filter: &qdrant.Filter{
				Must: []*qdrant.Condition{
					qdrant.NewMatchInt("shop_id", rand.Int63()%300000),
					qdrant.NewMatchInt("groups", rand.Int63()%300000),
				},
			},
			Params: &qdrant.SearchParams{
				HnswEf: proto.Uint64(300),
				Quantization: &qdrant.QuantizationSearchParams{
					Rescore:      proto.Bool(true),
					Oversampling: proto.Float64(2.0),
				},
			},
			WithPayload: qdrant.NewWithPayload(true),
			WithVectors: qdrant.NewWithVectors(true),
			Limit:       proto.Uint64(100),
		}
		result = append(result, newPoint)
	}
	out, err := json.Marshal(result)
	if err != nil {
		log.Fatalf("error on json marshal %s", err)
	}
	fmt.Println(string(out))
}
