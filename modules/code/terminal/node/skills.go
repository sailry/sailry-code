package terminalnode

import (
	"embed"
	"sync"

	"github.com/sailry/sailry-harness/sdk/native/node"
)

//go:embed skills/python/SKILL.md
var skillsFS embed.FS

var _ node.SkillProvider = module{}

var loadSkills = sync.OnceValue(func() []node.BuiltInSkill {
	skill, err := node.LoadBuiltInSkill(skillsFS, "commands", "python")
	if err != nil {
		return nil
	}
	return []node.BuiltInSkill{skill}
})

func (module) Skills() []node.BuiltInSkill { return loadSkills() }
