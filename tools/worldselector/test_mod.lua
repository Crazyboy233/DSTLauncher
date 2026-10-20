-- 世界选择器 mod 的 stub 验证脚本（开发期用，不进 mod）
--
-- 目标：不启动游戏，把 modmain.lua 在两套环境里各跑一遍，覆盖「服务端列表编码 /
-- 迁移校验 / 客户端面板渲染与点击」这几条容易写错的路。它只能抓逻辑和 API 用法
-- 层面的错误，抓不到引擎行为（落点、迁移时序），那部分必须上服验证。
--
-- 用法：lua tools/worldselector/test_mod.lua

local MOD_PATH = "mods/worldselector/modmain.lua"

local failures = 0
local function check(name, cond, detail)
    if cond then
        print("  ok   " .. name)
    else
        failures = failures + 1
        print("  FAIL " .. name .. (detail and ("  -> " .. tostring(detail)) or ""))
    end
end

--------------------------------------------------------------------------
-- 假控件：只实现 modmain 用到的那些方法
--------------------------------------------------------------------------
local function NewWidget(name)
    local w = { name = name, children = {}, pos = { 0, 0 } }
    w.inst = { valid = true }
    w.inst.IsValid = function() return w.inst.valid end
    w.inst.DoPeriodicTask = function(_, _, fn) w.task = fn end

    function w:AddChild(c) table.insert(self.children, c); c.parent = self; return c end
    function w:KillAllChildren() self.children = {} end
    function w:Kill() self.inst.valid = false end
    function w:SetPosition(x, y) self.pos = { x, y } end
    function w:MoveToFront() end
    function w:SetTint(r, g, b, a) self.tint = { r, g, b, a } end
    function w:SetSize(x, y) self.size = { x, y } end
    function w:ForceImageSize(x, y) self.size = { x, y } end
    function w:SetOnClick(fn) self.onclick = fn end
    function w:Disable() self.disabled = true end
    function w:Enable() self.disabled = false end
    function w:SetHAlign() end
    function w:SetString(s) self.text = s end
    function w:Show() end
    function w:Hide() end
    return w
end

--------------------------------------------------------------------------
-- 环境工厂
--------------------------------------------------------------------------
local function MakeEnv(opts)
    local env = {}
    local sent_to_client = {}
    local sent_to_server = {}
    local pushed_events = {}
    local console = {}

    env.print = function(...) console[#console + 1] = table.concat({ ... }, " ") end
    env.string = string
    env.table = table
    env.math = math
    env.type = type
    env.pairs = pairs
    env.ipairs = ipairs
    env.tostring = tostring
    env.tonumber = tonumber
    env.pcall = pcall
    env.select = select

    env.GLOBAL = {
        TheNet = {
            IsDedicated = function() return opts.dedicated end,
            GetIsClient = function() return not opts.dedicated end,
        },
        TheShard = { GetShardId = function() return opts.self_id end },
        TheWorld = {
            PushEvent = function(_, name, data)
                pushed_events[#pushed_events + 1] = { name = name, data = data }
            end,
        },
        GetTime = function() return opts.now end,
        Shard_GetConnectedShards = function() return opts.shards end,
        Shard_IsWorldAvailable = function(id)
            local s = opts.shards[tostring(id)]
            return s ~= nil and s.ready == true
        end,
        ANCHOR_LEFT = "left",
        ANCHOR_RIGHT = "right",
        TITLEFONT = "bp100",
        DEFAULTFONT = "opensans",
        BODYTEXTFONT = "stint-ucr",
        ThePlayer = opts.player,
    }

    env.require = function(path)
        if path == "widgets/widget" then return function(name) return NewWidget(name) end end
        if path == "widgets/image" then return function() return NewWidget("Image") end end
        if path == "widgets/imagebutton" then
            return function()
                local b = NewWidget("ImageButton")
                b.image = b:AddChild(NewWidget("Image"))
                return b
            end
        end
        if path == "widgets/text" then
            return function(_, _, text) local w = NewWidget("Text"); w.text = text; return w end
        end
        error("unexpected require: " .. tostring(path))
    end

    env.AddModRPCHandler = function(ns, name, fn) env._server_rpc = env._server_rpc or {}; env._server_rpc[ns .. "." .. name] = fn end
    env.AddClientModRPCHandler = function(ns, name, fn) env._client_rpc = env._client_rpc or {}; env._client_rpc[ns .. "." .. name] = fn end
    env.GetClientModRPC = function(ns, name) return ns .. "." .. name end
    env.GetModRPC = function(ns, name) return ns .. "." .. name end
    env.SendModRPCToClient = function(id, player, payload)
        sent_to_client[#sent_to_client + 1] = { id = id, player = player, payload = payload }
    end
    env.SendModRPCToServer = function(id, payload)
        sent_to_server[#sent_to_server + 1] = { id = id, payload = payload }
    end
    env.AddClassPostConstruct = function(_, fn) env._controls_post = fn end

    env._sent_to_client = sent_to_client
    env._sent_to_server = sent_to_server
    env._pushed_events = pushed_events
    env._console = console
    return env
end

local function LoadMod(env)
    local f = assert(io.open(MOD_PATH, "r"))
    local code = f:read("*a")
    f:close()
    local chunk = assert(load(code, MOD_PATH, "t", env))
    return chunk()
end

--------------------------------------------------------------------------
-- 服务端
--------------------------------------------------------------------------
print("服务端：")
local player = {
    userid = "KU_1",
    name = "小明",
    valid = true,
    tags = { player = true },
    components = {},
}
function player:IsValid() return self.valid end
function player:HasTag(t) return self.tags[t] == true or t == "player" end

local filter_called = false
local filter_result = nil
local filter_fn = nil
player.components.inventory = {
    DropEverythingByFilter = function(_, fn)
        filter_called = true
        filter_fn = fn
        filter_result = fn(player, { HasTag = function() return false end, components = {} })
    end,
}

local server = MakeEnv({
    dedicated = true,
    self_id = "1",
    now = 1000,
    player = nil,
    shards = {
        ["2"] = { ready = true, shard_name = "Caves", world = { { location = "cave" } } },
        ["3"] = { ready = false, shard_name = "World3", world = { { location = "forest" } } },
        ["4"] = { ready = true, shard_name = "World4", world = { { location = "forest" } } },
    },
})
LoadMod(server)

local query = server._server_rpc["WorldSelector.Query"]
local travel = server._server_rpc["WorldSelector.Travel"]
check("注册了 Query / Travel 两个 RPC", query ~= nil and travel ~= nil)

local client_player = { userid = "KU_9", name = "小红", valid = true }
function client_player:IsValid() return self.valid end
function client_player:HasTag(t) return t == "player" end

query(player)
local payload = server._sent_to_client[1] and server._sent_to_client[1].payload
check("列表只含已连接的世界（未就绪的 id=3 被排除）", payload ~= nil and payload:find("3\t") == nil, payload)
check("列表含洞穴与自定义世界，名字已翻译", payload == "2\t洞穴\tcave\n4\tWorld4\tforest", payload)
check("列表回给发请求的玩家", server._sent_to_client[1].player == player)

travel(player, "1")
check("迁到自己在的世界被拒", server._sent_to_client[2].payload == "你已经在这个世界了", server._sent_to_client[2].payload)

travel(player, "3")
check("迁到未连接的世界被拒", server._sent_to_client[3].payload == "这个世界当前不可进入", server._sent_to_client[3].payload)

travel(player, "2")
check("合法迁移：推了 ms_playerdespawnandmigrate", server._pushed_events[1] ~= nil
    and server._pushed_events[1].name == "ms_playerdespawnandmigrate")
check("迁移事件带的是发请求的玩家与字符串 worldid",
    server._pushed_events[1].data.player == player and server._pushed_events[1].data.worldid == "2")
check("迁移前跑过不可迁移物品过滤", filter_called == true)
check("普通物品不丢弃（filter 返回 false）", filter_result == false)
if filter_fn ~= nil then
    local irreplaceable = { components = {}, HasTag = function(_, t) return t == "irreplaceable" end }
    check("irreplaceable 物品会被丢下", filter_fn(player, irreplaceable) == true)
end

travel(player, "2")
check("冷却期内第二次迁移被拒", (server._sent_to_client[4].payload or ""):find("还要等") ~= nil,
    server._sent_to_client[4].payload)

--------------------------------------------------------------------------
-- 客户端
--------------------------------------------------------------------------
print("客户端：")
local hud_root = NewWidget("hud_root")
local player_for_ui = {
    valid = true,
    HUD = { root = hud_root },
}
function player_for_ui:IsValid() return self.valid end

local client = MakeEnv({
    dedicated = false,
    self_id = "1",
    now = 1000,
    player = player_for_ui,
    shards = {},
})
LoadMod(client)

check("客户端注册了 List / Denied 两个下行 RPC", client._client_rpc["WorldSelector.List"] ~= nil
    and client._client_rpc["WorldSelector.Denied"] ~= nil)

local controls = NewWidget("controls")
controls.owner = player_for_ui
controls.bottomright_root = NewWidget("bottomright")
client._controls_post(controls)

local btn = controls.worldselector_btn
check("HUD 上挂了「世界」按钮", btn ~= nil)
check("按钮挂了点击回调", btn ~= nil and btn.onclick ~= nil)

btn.onclick()
check("点按钮打开了面板并挂到 HUD.root", #hud_root.children == 1)
check("打开面板立刻请求一次列表", client._sent_to_server[1] ~= nil
    and client._sent_to_server[1].id == "WorldSelector.Query")

local panel = hud_root.children[1]
client._client_rpc["WorldSelector.List"]("2\t洞穴\tcave\n4\tWorld4\tforest")
local rows = {}
for _, child in ipairs(panel.children) do
    if child.name == "ImageButton" and child.onclick ~= nil and child.size and child.size[1] == 344 then
        rows[#rows + 1] = child
    end
end
check("两行世界都渲染出来了", #rows == 2, #rows)

if #rows == 2 then
    rows[2].onclick()
    local sent = client._sent_to_server[#client._sent_to_server]
    check("点第二行发出 Travel 且 worldid 正确", sent.id == "WorldSelector.Travel" and sent.payload == "4",
        sent.payload)
    check("点完锁定面板（按钮禁用）", rows[1].disabled == true)
    client._client_rpc["WorldSelector.Denied"]("这个世界当前不可进入")
    check("被拒后按钮恢复可点", rows[1].disabled == false)

    client._client_rpc["WorldSelector.List"]("2\t洞穴\tcave\n4\tWorld4\tforest\n5\tWorld5\tforest")
    check("轮询重绘不会冲掉错误提示", panel.status ~= nil and panel.status.text == "这个世界当前不可进入",
        panel.status and panel.status.text)
end

local same = panel.children
client._client_rpc["WorldSelector.List"]("2\t洞穴\tcave\n4\tWorld4\tforest\n5\tWorld5\tforest")
check("列表没变时不重建控件", panel.children == same)

print("")
if failures == 0 then
    print("全部通过")
else
    print(string.format("%d 项失败", failures))
    os.exit(1)
end
