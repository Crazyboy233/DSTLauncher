# 世界选择器（World Selector）v0.1

HUD 上点「世界」，选一个世界过去。**任意玩家可用，不需要管理员。**

## 装法

```
mods/worldselector/            ← 整个目录拷到专服的 mods/ 下
```

分片配置里启用（每个要开这个功能的分片都要有，因为 RPC handler 是分片各自注册的）：

```lua
-- <cluster>/<shard>/modoverrides.lua
return {
  ["worldselector"] = { enabled = true },
}
```

## 它做什么

- 列表来自 `Shard_GetConnectedShards()`：**只显示当前真连着的分片**，没起来的世界不会出现（不是灰掉，是不在表里）。
- 点一行 → 服务端校验 → 推 `ms_playerdespawnandmigrate`，迁移由引擎完成，物品一起带走。
- 冷却 30 秒；迁移进行中按钮锁死；被拒的理由显示在面板底部。
- `irreplaceable` 物品和带宠物的物品会被放在原地（与原版走洞穴入口一致）。

## 已知边界

- **落点 = 目标世界的默认出生点**。目标世界没有 id 对应的 `worldmigrator` 门时无法指定落点，后续版本再补「世界之门」。
- **不检查目标世界人数上限**，也没有二次确认。
- **客户端也得装这个 mod**。本地 mod 没有工坊 id，无法自动下发：
  - `all_clients_require_mod` 目前是 `false`，因为设成 `true` 会让没装 mod 的玩家**直接进不来**
    （客户端报 `SERVER_MODS_NOT_ON_WORKSHOP`，见 `networking.lua:484`）；
  - 客户端装好后还要在模组列表里手动启用——非工坊必需 mod 不会被自动启用；
  - 想让「任何玩家」都无感使用，只有上架创意工坊一条路，上架后再把该字段改成 `true`。

## 开发期验证

```bash
luac -p mods/worldselector/modmain.lua
lua tools/worldselector/test_mod.lua
```

stub 脚本只覆盖列表编码、迁移校验、面板渲染与点击这些逻辑层；落点和迁移时序必须上服实测。
