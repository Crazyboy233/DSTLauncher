name = "世界选择器"
description = "HUD 上点一下，自己选一个世界过去。任意玩家可用，不需要管理员。"
author = "chihiro"
version = "0.1.3"

api_version = 10
dst_compatible = true
dont_starve_compatible = false
reign_of_giants_compatible = false

-- 迁移逻辑在服务端、列表界面在客户端，所以是双端 mod。
--
-- all_clients_require_mod = true：本 mod 已上架创意工坊，客户端加入服务器时会
-- 自动下载并启用，真正做到「任何玩家都能点」。
-- 注意：本地开发版（没有工坊 id）不能设 true，否则没装 mod 的客户端会卡在
-- SERVER_MODS_NOT_ON_WORKSHOP（networking.lua:484）进不来。
all_clients_require_mod = true
client_only_mod = false
server_filter_tags = { "world selector", "shard" }

icon_atlas = ""
icon = ""
