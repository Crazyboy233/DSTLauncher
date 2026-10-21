-- 世界选择器 v0.1
--
-- 目标：服务器里任何玩家（不需要管理员）在 HUD 点「世界」，自己选一个世界过去。
--
-- 结构：
--   客户端：HUD 右下角按钮 → 浮层列表 → 点某一行只发 RPC
--   服务端：收 RPC → 校验 → push ms_playerdespawnandmigrate，迁移由引擎接管
--
-- 为什么不用 worldmigrator（原版洞穴入口那套）：它要求地图上有一个真实传送门实体，
-- 还会被 Shard_UpdatePortalState 自动绑到「第一个连上的分片」，位置和归属都不受控。
-- 直接 push 迁移事件不需要任何实体，落点交给目标世界决定（没有对应门 → 默认出生点）。
--
-- 列表数据源是 Shard_GetConnectedShards()：没起来 / 没连上的世界根本不在里面，
-- 所以「列表里没有它」和「点不进去」是同一件事，不需要额外的在线状态字段。

local RPC_NS = "WorldSelector"

-- DST 的 mod 沙箱环境没有注入 tonumber（tostring/pairs/type 都有，唯独 tonumber
-- 是 nil，2026-10-21 实测报 "attempt to call global 'tonumber' (a nil value)"）。
-- 从 GLOBAL 取一份，避免在排序闭包里踩到这个坑。
local tonumber = GLOBAL.tonumber

-- 同一玩家两次迁移之间的最小间隔（秒）。迁移会序列化整个玩家存档，连点很亏。
local COOLDOWN_SECONDS = 30

-- 分片目录名 → 玩家看得懂的名字；未命中的自定义分片显示目录名
local SHARD_NAME = {
    Master = "地表",
    Caves = "洞穴",
}
local LOCATION_LABEL = {
    forest = "地表",
    cave = "洞穴",
}

-- 客户端面板状态：下行 RPC handler（两端都注册）与客户端 UI 区共享，
-- 必须声明在共享区。服务端上它们保持 nil，永远不会被触碰。
local the_panel = nil
local the_list = nil
-- 面板重绘函数由客户端 UI 区赋值；服务端上保持 nil（handler 不会在服务端执行）
local RenderPanel = nil

-- ===================== 客户端下行 RPC（两端都必须注册） =====================
--
-- 引擎按注册表给 mod RPC 分配 id：CLIENT_MOD_RPC[namespace][name] 只在
-- AddClientModRPCHandler 执行后才存在。专用服务器不跑客户端 UI 区，
-- 但 Query 处理里要调 GetClientModRPC 取 id 才能发 List——服务端不注册的话，
-- GetClientModRPC 直接对 nil 索引报错（networkclientrpc.lua:1973
-- "attempt to index field '?' (a nil value)"，2026-10-21 实测踩坑）。
-- 处理函数本体只在客户端收到 RPC 时执行，服务端注册无副作用。
AddClientModRPCHandler(RPC_NS, "List", function(payload)
    payload = payload or ""
    -- 5 秒一次的轮询里，列表没变就别重建控件（重建会闪一下）
    if payload == the_list then
        return
    end
    the_list = payload
    if the_panel ~= nil and the_panel.inst:IsValid() then
        RenderPanel(the_panel)
    end
end)

AddClientModRPCHandler(RPC_NS, "Denied", function(reason)
    if the_panel ~= nil and the_panel.inst:IsValid() then
        the_panel.locked = false
        the_panel.notice = tostring(reason or "无法迁移")
        for _, btn in ipairs(the_panel.buttons or {}) do
            btn:Enable()
        end
        if the_panel.status ~= nil then
            the_panel.status:SetString(the_panel.notice)
        end
    end
end)

-- ===================== 服务端 =====================

local last_travel_at = {}

local function LabelOf(shard_name, location)
    if shard_name ~= nil and SHARD_NAME[shard_name] ~= nil then
        return SHARD_NAME[shard_name]
    end
    if shard_name ~= nil and shard_name ~= "" then
        return shard_name
    end
    return LOCATION_LABEL[location] or "未知世界"
end

-- 列表编码：每行 "id\t名字\t类型"，行间 \n。
-- 不用 RPC 传 table，字符串在各版本都稳定，也方便在日志里直接看懂。
local function BuildListPayload()
    if GLOBAL.Shard_GetConnectedShards == nil then
        return ""
    end

    local self_id = tostring(GLOBAL.TheShard:GetShardId())
    local entries = {}

    for world_id, info in pairs(GLOBAL.Shard_GetConnectedShards()) do
        local id = tostring(world_id)
        -- 自己不在 ShardConnected 里只有一种可能：这个表连自己都还没登记，跳过自检
        if id ~= self_id and info.ready then
            local location = "forest"
            local world = info.world
            if type(world) == "table" and type(world[1]) == "table" and world[1].location ~= nil then
                location = world[1].location
            end
            entries[#entries + 1] = {
                id = id,
                label = LabelOf(info.shard_name, location),
                location = location,
            }
        end
    end

    -- 按 id 数字序排；id 是字符串（SHARDID.MASTER == "1"），直接排会得到 1,10,2
    table.sort(entries, function(a, b)
        local na, nb = tonumber(a.id) or 0, tonumber(b.id) or 0
        if na ~= nb then
            return na < nb
        end
        return a.id < b.id
    end)

    local lines = {}
    for i = 1, #entries do
        local e = entries[i]
        lines[i] = string.format("%s\t%s\t%s", e.id, e.label, e.location)
    end
    return table.concat(lines, "\n")
end

-- 原版 worldmigrator 的 CanInventoryItemMigrate 口径：不可迁移的东西留在原地。
-- 我们自己 push 事件等于跳过它，所以这里补一份，否则 irreplaceable 物品会被带过界。
local function DropUnmigratable(player)
    local inventory = player.components.inventory
    if inventory == nil then
        return
    end
    inventory:DropEverythingByFilter(function(_, item)
        if item == nil then
            return false
        end
        local petowner = item.components.migrationpetowner
        if petowner ~= nil and petowner.GetPet ~= nil and petowner:GetPet() ~= nil then
            return true
        end
        return item:HasTag("irreplaceable")
    end)
end

local function TryTravel(player, raw_world_id)
    if player == nil or not player:IsValid() or not player:HasTag("player") then
        return false, "玩家状态无效"
    end

    local world_id = tostring(raw_world_id or "")
    if world_id == "" then
        return false, "目标世界无效"
    end
    if world_id == tostring(GLOBAL.TheShard:GetShardId()) then
        return false, "你已经在这个世界了"
    end
    if GLOBAL.Shard_IsWorldAvailable == nil or not GLOBAL.Shard_IsWorldAvailable(world_id) then
        return false, "这个世界当前不可进入"
    end
    -- 引擎在迁移期间会置这个标记（playerspawner.lua），期间再推事件会重复序列化
    if player._despawning then
        return false, "正在迁移中"
    end

    local now = GLOBAL.GetTime()
    local last = last_travel_at[player.userid]
    if last ~= nil and now - last < COOLDOWN_SECONDS then
        return false, string.format("还要等 %d 秒", math.ceil(COOLDOWN_SECONDS - (now - last)))
    end

    last_travel_at[player.userid] = now
    DropUnmigratable(player)

    GLOBAL.TheWorld:PushEvent("ms_playerdespawnandmigrate", {
        player = player,
        portalid = nil,
        worldid = world_id,
    })
    print(string.format("[WorldSelector] %s -> world %s", tostring(player.name or player.userid), world_id))
    return true
end

-- 客户端打开面板时来要一次列表
AddModRPCHandler(RPC_NS, "Query", function(player)
    if player == nil or not player:IsValid() then
        return
    end
    SendModRPCToClient(GetClientModRPC(RPC_NS, "List"), player, BuildListPayload())
end)

-- 点某一行：[player] 由引擎给出（真实发请求的那个玩家），绝不接受客户端传身份
AddModRPCHandler(RPC_NS, "Travel", function(player, world_id)
    local ok, err = TryTravel(player, world_id)
    if not ok and player ~= nil and player:IsValid() then
        SendModRPCToClient(GetClientModRPC(RPC_NS, "Denied"), player, tostring(err or "无法迁移"))
    end
end)

-- ===================== 客户端 UI =====================

if GLOBAL.TheNet == nil or GLOBAL.TheNet:IsDedicated() then
    return
end

local Widget = require("widgets/widget")
local Image = require("widgets/image")
local ImageButton = require("widgets/imagebutton")
local Text = require("widgets/text")

-- images/global.xml 里的 square.tex 是纯白方块，靠 SetTint 上色，不需要额外美术资源
local ATLAS = "images/global.xml"
local ATLAS_IMAGE = "square.tex"

local BUTTON_SIZE = { 96, 36 }
-- bottomright_root 的原点在屏幕右下角（controls.lua 里 SetHAnchor(ANCHOR_RIGHT) +
-- SetVAnchor(ANCHOR_BOTTOM)），x 负数向左、y 正数向上；原版小地图按钮在 (-60, 70)。
-- y 必须为正，负值会把按钮放到屏幕底边之外（首版 -50 就是这个坑）。
local BUTTON_POS = { -170, 60 }

local PANEL_W = 380
local ROW_W = 344
local ROW_H = 40
local HEADER_H = 56
local FOOTER_H = 40
local MAX_ROWS = 8
local REFRESH_SECONDS = 5

local COL_TEXT = { 0.95, 0.92, 0.85, 1 }
local COL_DIM = { 0.70, 0.66, 0.58, 1 }
local COL_STATUS = { 0.78, 0.72, 0.60, 1 }

local ANCHOR_LEFT = GLOBAL.ANCHOR_LEFT
local ANCHOR_RIGHT = GLOBAL.ANCHOR_RIGHT
local TITLEFONT = GLOBAL.TITLEFONT
local DEFAULTFONT = GLOBAL.DEFAULTFONT
local BODYTEXTFONT = GLOBAL.BODYTEXTFONT

local function ParseList(payload)
    local entries = {}
    for line in string.gmatch(payload or "", "[^\n]+") do
        local id, label, location = string.match(line, "^(.-)\t(.-)\t(.-)$")
        if id ~= nil then
            entries[#entries + 1] = { id = id, label = label, location = location }
        end
    end
    return entries
end

local function SendQuery()
    SendModRPCToServer(GetModRPC(RPC_NS, "Query"))
end

local function MakeSquareButton(parent, w, h, tint, onclick)
    local btn = parent:AddChild(ImageButton(
        ATLAS, ATLAS_IMAGE, ATLAS_IMAGE, ATLAS_IMAGE, ATLAS_IMAGE, ATLAS_IMAGE))
    btn.image:SetTint(tint[1], tint[2], tint[3], tint[4])
    btn:ForceImageSize(w, h)
    btn:SetOnClick(onclick)
    return btn
end

-- 面板整体重绘：世界数量变了高度就得变，逐行增删反而更绕。
-- 注意：RenderPanel 是共享区前置声明的局部（下行 RPC handler 也要调它），
-- 这里只能赋值，不能再写 local。
RenderPanel = function(root)
    root:KillAllChildren()
    root.buttons = {}
    -- 迁移请求已发出但结果没回来时重绘，不能把按钮又放开
    local locked = root.locked == true

    local entries = ParseList(the_list)
    local shown = math.min(#entries, MAX_ROWS)
    local panel_h = HEADER_H + shown * ROW_H + FOOTER_H

    local bg = root:AddChild(Image(ATLAS, ATLAS_IMAGE))
    bg:SetTint(0.06, 0.05, 0.04, 0.92)
    bg:SetSize(PANEL_W, panel_h)

    local title = root:AddChild(Text(TITLEFONT, 26, "选择世界", COL_TEXT))
    title:SetPosition(0, panel_h / 2 - 30)

    local close = MakeSquareButton(root, 30, 30, { 0.20, 0.18, 0.16, 0.90 }, function()
        root:Kill()
        the_panel = nil
    end)
    close:SetPosition(PANEL_W / 2 - 26, panel_h / 2 - 28)
    close:AddChild(Text(BODYTEXTFONT, 22, "×", COL_TEXT))

    local top_of_rows = panel_h / 2 - HEADER_H
    for i = 1, shown do
        local e = entries[i]
        local y = top_of_rows - (i - 0.5) * ROW_H
        local btn = MakeSquareButton(root, ROW_W, ROW_H - 6, { 0.17, 0.15, 0.12, 0.95 }, function()
            if root.locked then
                return
            end
            root.locked = true
            root.notice = nil
            for _, b in ipairs(root.buttons or {}) do
                b:Disable()
            end
            if root.status ~= nil then
                root.status:SetString("正在前往…")
            end
            SendModRPCToServer(GetModRPC(RPC_NS, "Travel"), e.id)
        end)
        btn:SetPosition(0, y)

        local label = btn:AddChild(Text(DEFAULTFONT, 22, e.label, COL_TEXT))
        label:SetHAlign(ANCHOR_LEFT)
        label:SetPosition(-ROW_W / 2 + 18, 0)

        local tag = btn:AddChild(Text(BODYTEXTFONT, 18, LOCATION_LABEL[e.location] or "", COL_DIM))
        tag:SetHAlign(ANCHOR_RIGHT)
        tag:SetPosition(ROW_W / 2 - 18, 0)

        root.buttons[#root.buttons + 1] = btn
    end

    if locked then
        for _, b in ipairs(root.buttons) do
            b:Disable()
        end
    end

    -- notice 由被拒时写入，下一次点击或重新打开面板才清掉：
    -- 5 秒一次的轮询重绘不能把错误提示冲掉，否则玩家还没看完就没了
    local status_text = root.notice
    if locked then
        status_text = "正在前往…"
    elseif status_text == nil and #entries == 0 then
        status_text = the_list == nil and "正在获取世界列表…" or "没有其他已连接的世界"
    elseif status_text == nil and shown < #entries then
        status_text = string.format("共 %d 个世界，只显示前 %d 个", #entries, MAX_ROWS)
    elseif status_text == nil then
        status_text = "选择后立即迁移，物品会一起带过去"
    end

    local status = root:AddChild(Text(BODYTEXTFONT, 18, status_text, COL_STATUS))
    status:SetPosition(0, -panel_h / 2 + 22)
    root.status = status
end

local function OpenPanel(owner)
    local hud = owner ~= nil and owner.HUD or nil
    if hud == nil or hud.root == nil then
        return
    end

    -- 自建锚点节点，不依赖 hud.root 的坐标系：直接锚定屏幕中心、按比例缩放
    -- （与 controls.lua 里 bottomright_root 的官方做法一致），
    -- 窗口化 / 任意分辨率下面板都居中且比例正确。挂在固定坐标系的父节点上
    -- 用 SetPosition 落点不可控（实测跑到过左下角屏幕外）。
    local root = Widget("worldselector_panel")
    root:SetScaleMode(GLOBAL.SCALEMODE_PROPORTIONAL)
    root:SetHAnchor(GLOBAL.ANCHOR_MIDDLE)
    root:SetVAnchor(GLOBAL.ANCHOR_MIDDLE)
    root:SetMaxPropUpscale(GLOBAL.MAX_HUD_SCALE)
    the_panel = root
    RenderPanel(root)
    hud.root:AddChild(root)
    root:MoveToFront()

    SendQuery()
    -- 面板开着时定期刷新：别的世界可能刚起来或正好关了
    root.inst:DoPeriodicTask(REFRESH_SECONDS, function()
        if root.inst:IsValid() then
            SendQuery()
        end
    end)
end

local function TogglePanel(owner)
    if the_panel ~= nil and the_panel.inst:IsValid() then
        the_panel:Kill()
        the_panel = nil
        return
    end
    OpenPanel(owner)
end

-- HUD 按钮：挂在右下角（小地图旁边），每个玩家自己点
AddClassPostConstruct("widgets/controls", function(controls)
    if controls.worldselector_btn ~= nil then
        return
    end

    local btn = MakeSquareButton(controls.bottomright_root, BUTTON_SIZE[1], BUTTON_SIZE[2],
        { 0.17, 0.15, 0.12, 0.85 }, function()
            TogglePanel(controls.owner or GLOBAL.ThePlayer)
        end)
    btn:SetPosition(BUTTON_POS[1], BUTTON_POS[2])
    btn:AddChild(Text(DEFAULTFONT, 20, "世界", COL_TEXT))

    controls.worldselector_btn = btn
end)
