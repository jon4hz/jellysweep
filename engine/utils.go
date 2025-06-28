package engine

import (
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

// triggerTagIDs returns tag IDs that should trigger deletion based on their date labels.
func (e *Engine) triggerTagIDs(tags map[int32]string) ([]int32, error) {
	triggerTagIDs := make([]int32, 0)
	for id, tag := range tags {
		if strings.HasPrefix(tag, jellysweepTagPrefix) {
			tagLabel := strings.TrimPrefix(tag, jellysweepTagPrefix)

			// Parse the date from the tag label
			dateStr := strings.TrimSuffix(tagLabel, "-")
			date, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				log.Warnf("failed to parse date from tag label %s: %v", tagLabel, err)
				continue
			}
			// Check if the date is in the past
			if date.Before(time.Now()) {
				// If the date is in the past, add the tag ID to the trigger list
				triggerTagIDs = append(triggerTagIDs, id)
			} else {
				log.Debugf("Skipping tag %s as it is not yet due for deletion", tagLabel)
			}
		}
	}
	return triggerTagIDs, nil
}
