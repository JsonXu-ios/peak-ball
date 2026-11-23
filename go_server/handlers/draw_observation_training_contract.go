package handlers

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"go_server/internal/drawmodeldata"
	"strings"
)

// Include preprocessing, not just the tree extractor: changes to profit/form
// calculations must also invalidate a fast no-change startup decision.
//
//go:embed analysis*.go platform_decision.go draw*.go
var drawTrainingProducerSources embed.FS
var drawTrainingFeatureHash = buildDrawTrainingFeatureHash()

func buildDrawTrainingFeatureHash() string {
	h := sha256.New()
	h.Write([]byte(drawmodeldata.FeatureCodeSHA256()))
	h.Write([]byte(drawmodeldata.VenueFeatureCodeSHA256()))
	files, err := drawTrainingProducerSources.ReadDir(".")
	if err != nil {
		panic(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		body, err := drawTrainingProducerSources.ReadFile(file.Name())
		if err != nil {
			panic(err)
		}
		h.Write([]byte(file.Name()))
		h.Write([]byte{0})
		h.Write(body)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
