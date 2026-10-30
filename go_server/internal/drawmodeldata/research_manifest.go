package drawmodeldata

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
)

//go:embed research_manifest.json
var researchManifestJSON []byte

//go:embed features.go market_features.go history_features.go research_manifest.json
var featureContractSources embed.FS

// ResearchManifest describes all 530 research inputs, including those not used
// by the production model. It contains definitions only, no match data.
func ResearchManifest() json.RawMessage { return append(json.RawMessage(nil), researchManifestJSON...) }

func ArtifactSHA256() string {
	hash := sha256.Sum256(modelJSON)
	return hex.EncodeToString(hash[:])
}

func FeatureCodeSHA256() string {
	hash := sha256.New()
	for _, name := range []string{"features.go", "market_features.go", "history_features.go", "research_manifest.json"} {
		body, err := featureContractSources.ReadFile(name)
		if err != nil {
			panic(err)
		}
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write(body)
	}
	hash.Write(modelJSON)
	return hex.EncodeToString(hash.Sum(nil))
}
