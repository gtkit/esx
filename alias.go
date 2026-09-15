package esx

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/elastic/go-elasticsearch/v9/typedapi/indices/updatealiases"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// AliasState 是一个别名当前的指向状态。
type AliasState struct {
	// Name 是被解析的别名。
	Name string
	// Targets 是该别名指向的索引，按名字升序排列，使相同集群状态产生相同结果。
	Targets []string
	// ConcreteIndex 为真时，Name 上并没有别名，而是存在一个同名的具体索引，
	// Targets 就是它自己。切换别名时这种情形要走 remove_index 而非 remove。
	ConcreteIndex bool
}

// ResolveAlias 解析别名当前指向哪些索引。
//
// 别名与同名具体索引都不存在时返回空 Targets 与 nil 错误。
func (c *Client) ResolveAlias(ctx context.Context, alias string) (AliasState, error) {
	state := AliasState{Name: alias}
	if alias == "" {
		return state, invalid("resolve alias", errors.New("alias name is required"))
	}

	res, err := c.typed.Indices.GetAlias().Name(alias).Do(ctx)
	if err != nil {
		if statusOf(err) != http.StatusNotFound {
			return state, wrapErr("resolve alias", alias, err)
		}
		// 没有这个别名。可能是历史遗留的同名具体索引，查一次再下结论。
		exists, existsErr := c.IndexExists(ctx, alias)
		if existsErr != nil {
			return state, existsErr
		}
		if exists {
			state.Targets = []string{alias}
			state.ConcreteIndex = true
		}
		return state, nil
	}

	for index, detail := range res {
		if _, ok := detail.Aliases[alias]; ok {
			state.Targets = append(state.Targets, index)
		}
	}
	slices.Sort(state.Targets)
	return state, nil
}

// SwitchAlias 把别名切换到 newIndex。
//
// 移除旧目标与添加新目标在同一个 _aliases 请求内完成，别名不会出现不指向任何索引的
// 中间状态——分两次请求会让该窗口内的查询报 index_not_found_exception。
// 切换后 newIndex 是该别名的写索引。
func (c *Client) SwitchAlias(ctx context.Context, state AliasState, newIndex string) error {
	if state.Name == "" {
		return invalid("switch alias", errors.New("alias name is required"))
	}
	if newIndex == "" {
		return invalid("switch alias", errNoIndexName)
	}

	req := updatealiases.NewRequest()
	req.Actions = buildSwitchActions(state, newIndex)

	if _, err := c.typed.Indices.UpdateAliases().Request(req).Do(ctx); err != nil {
		return wrapErr("switch alias", state.Name, err)
	}
	return nil
}

func buildSwitchActions(state AliasState, newIndex string) []types.IndicesAction {
	actions := make([]types.IndicesAction, 0, len(state.Targets)+1)
	for _, current := range state.Targets {
		if current == newIndex {
			// 已经是目标索引，不要先 remove 再 add 把自己摘掉。
			continue
		}
		if state.ConcreteIndex && current == state.Name {
			// 名字上是具体索引而不是别名，只能整个删掉，remove 会因为找不到别名而失败。
			actions = append(actions, types.IndicesAction{
				RemoveIndex: &types.RemoveIndexAction{Index: new(current)},
			})
			continue
		}
		actions = append(actions, types.IndicesAction{
			Remove: &types.RemoveAction{Index: new(current), Alias: new(state.Name)},
		})
	}
	actions = append(actions, types.IndicesAction{
		Add: &types.AddAction{
			Index:        new(newIndex),
			Alias:        new(state.Name),
			IsWriteIndex: new(true),
		},
	})
	return actions
}
