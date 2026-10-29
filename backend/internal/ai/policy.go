package ai

import (
	"context"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

const FujiChainID = uint64(43113)

type StaticPolicyEngine struct {
	readTools  map[string]struct{}
	writeTools map[string]struct{}
}

func NewStaticPolicyEngine(
	readToolNames []string,
	writeToolNames []string,
) *StaticPolicyEngine {
	engine := &StaticPolicyEngine{
		readTools:  make(map[string]struct{}),
		writeTools: make(map[string]struct{}),
	}

	for _, name := range readToolNames {
		name = strings.TrimSpace(name)

		if name != "" {
			engine.readTools[name] = struct{}{}
		}
	}

	for _, name := range writeToolNames {
		name = strings.TrimSpace(name)

		if name != "" {
			engine.writeTools[name] = struct{}{}
		}
	}

	return engine
}

func (e *StaticPolicyEngine) Evaluate(
	ctx context.Context,
	request PolicyRequest,
) (PolicyDecision, error) {
	if err := ctx.Err(); err != nil {
		return PolicyDecision{}, err
	}

	request.ToolName = strings.TrimSpace(request.ToolName)
	request.WalletAddress = strings.TrimSpace(request.WalletAddress)

	if request.ChainID != FujiChainID {
		return PolicyDecision{
			Allowed:              false,
			RequiresConfirmation: false,
			Reason:               "only Avalanche Fuji chain ID 43113 is allowed",
		}, nil
	}

	if !common.IsHexAddress(request.WalletAddress) {
		return PolicyDecision{
			Allowed:              false,
			RequiresConfirmation: false,
			Reason:               "wallet address is invalid",
		}, nil
	}

	// Check write tools first. If a tool is accidentally present in both
	// allowlists, the safer write policy takes precedence.
	if _, allowed := e.writeTools[request.ToolName]; allowed {
		return PolicyDecision{
			Allowed:              true,
			RequiresConfirmation: true,
			Reason: "write tool may only prepare an action; " +
				"explicit wallet confirmation is required",
		}, nil
	}

	if _, allowed := e.readTools[request.ToolName]; allowed {
		return PolicyDecision{
			Allowed:              true,
			RequiresConfirmation: false,
			Reason:               "read tool is allowlisted",
		}, nil
	}

	return PolicyDecision{
		Allowed:              false,
		RequiresConfirmation: false,
		Reason:               "tool is not allowlisted",
	}, nil
}

var _ PolicyEngine = (*StaticPolicyEngine)(nil)
