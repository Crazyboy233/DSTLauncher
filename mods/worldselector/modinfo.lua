name = "世界选择器"
description = "HUD 上点一下，自己选一个世界过去。任意玩家可用，不需要管理员。"
author = "chihiro"
version = "0.1.0"

api_version = 10
dst_compatible = true
dont_starve_compatible = false
reign_of_giants_compatible = false

-- 迁移逻辑在服务端、列表界面在客户端，所以是双端 mod。
--
-- all_clients_require_mod 必须留 false：本地 mod 没有工坊 id，客户端拿不到就会
-- 直接卡在 SERVER_MODS_NOT_ON_WORKSHOP（networking.lua:484）进不来。
-- 等这个 mod 上架创意工坊后，再改成 true —— 那时客户端才会自动下载并启用，
-- 也才真正做到「任何玩家都能点」。
all_clients_require_mod = false
client_only_mod = false
server_filter_tags = { "world selector", "shard" }

icon_atlas = ""
icon = ""
