package config

import (
	"strconv"
	"strings"

	"github.com/jesseduffield/lazygit/pkg/utils"
)

type PagerConfig struct {
	getUserConfig func() *UserConfig
	pagerIndex    int
}

func NewPagerConfig(getUserConfig func() *UserConfig) *PagerConfig {
	return &PagerConfig{getUserConfig: getUserConfig}
}

func (self *PagerConfig) currentPagerConfig() *PagingConfig {
	pagers := self.getUserConfig().Git.Pagers
	if len(pagers) == 0 {
		return nil
	}

	// Guard against the pager index being out of range, which can happen if the user
	// has removed pagers from their config file while lazygit is running.
	if self.pagerIndex >= len(pagers) {
		self.pagerIndex = 0
	}

	return &pagers[self.pagerIndex]
}

func (self *PagerConfig) GetPagerCommand(width int) string {
	currentPagerConfig := self.currentPagerConfig()
	if currentPagerConfig == nil {
		return ""
	}

	templateValues := map[string]string{
		"columnWidth": strconv.Itoa(width/2 - 6),
	}

	pagerTemplate := string(currentPagerConfig.Pager)
	return utils.ResolvePlaceholderString(pagerTemplate, templateValues)
}

func (self *PagerConfig) GetColorArg() string {
	currentPagerConfig := self.currentPagerConfig()
	if currentPagerConfig == nil {
		return "always"
	}

	colorArg := currentPagerConfig.ColorArg
	if colorArg == "" {
		return "always"
	}
	return colorArg
}

func (self *PagerConfig) GetExternalDiffCommand() string {
	currentPagerConfig := self.currentPagerConfig()
	if currentPagerConfig == nil {
		return ""
	}
	return currentPagerConfig.ExternalDiffCommand
}

func (self *PagerConfig) GetUseExternalDiffGitConfig() bool {
	currentPagerConfig := self.currentPagerConfig()
	if currentPagerConfig == nil {
		return false
	}
	return currentPagerConfig.UseExternalDiffGitConfig
}

func (self *PagerConfig) StagingPagerEnabled() bool {
	currentPagerConfig := self.currentPagerConfig()
	if currentPagerConfig == nil {
		return false
	}
	// Only enable if there's actually a pager configured and staging view usage is enabled
	return currentPagerConfig.UseInStagingView && string(currentPagerConfig.Pager) != ""
}

func (self *PagerConfig) CyclePagers() {
	self.pagerIndex = (self.pagerIndex + 1) % len(self.getUserConfig().Git.Pagers)
}

func (self *PagerConfig) CurrentPagerIndex() (int, int) {
	return self.pagerIndex, len(self.getUserConfig().Git.Pagers)
}

// IsDeltaPager returns true if the current pager command appears to be delta.
// This is used to enable delta-specific features like decoration line detection.
func (self *PagerConfig) IsDeltaPager() bool {
	currentPagerConfig := self.currentPagerConfig()
	if currentPagerConfig == nil {
		return false
	}
	pagerCmd := strings.ToLower(string(currentPagerConfig.Pager))
	// Check for "delta" as a word (not just substring) to avoid false positives
	// Common patterns: "delta", "delta --dark", "/path/to/delta"
	return strings.Contains(pagerCmd, "delta")
}
