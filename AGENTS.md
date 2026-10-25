# AGENTS.md

> 本文件仅针对 **DSH（DeepSeek Harness）** 会话；其他工具/环境可忽略。

**DSH 必看**：在 DSH 里打包本项目，请按项目技能
[`dst-windows-packaging`](.dsh/skills/dst-windows-packaging/SKILL.md) 的流程；编译即本地
`go build -o dst-windows.exe .`（改过 `web/` 前端时先 `cd web; npm run build; cd ..`）。

## 注意

- **MOTW 标记 → SmartScreen 弹窗**：与签名、Low 标签都无关。只有「从网络下载」的文件才会被
  浏览器/聊天工具打上 `Zone.Identifier`（ZoneId=3）标记，双击才触发 SmartScreen「无法识别的
  应用」。本地编译直接落盘的 exe 没有该标记、不会弹；若 exe 经下载/传输渠道交付给用户，用户端
  双击弹窗属正常现象，不是编译问题，也无需在 README 说明。
