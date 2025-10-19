package utils

import (
	"strings"

	"github.com/xorvus/scrap-chat/types"
)

// BadgeInfo contains extracted badge information with labels and image URLs.
type BadgeInfo struct {
	Labels string
	Images string
}

// ExtractBadgeInfo extracts and formats badge information from a list of badges.
// Returns comma-separated labels and image URLs.
func ExtractBadgeInfo(badges []types.Badge) BadgeInfo {
	if len(badges) == 0 {
		return BadgeInfo{}
	}

	labels := make([]string, 0, len(badges))
	images := make([]string, 0, len(badges))

	for _, badge := range badges {
		labels = append(labels, badge.Label)
		if badge.IconURL != "" {
			images = append(images, badge.IconURL)
		}
	}

	return BadgeInfo{
		Labels: strings.Join(labels, ", "),
		Images: strings.Join(images, ", "),
	}
}

// IsVerifiedBadge checks if any badge in the list indicates verified status.
func IsVerifiedBadge(badges []types.Badge) bool {
	for _, badge := range badges {
		if strings.Contains(strings.ToLower(badge.Label), "verified") {
			return true
		}
	}
	return false
}
