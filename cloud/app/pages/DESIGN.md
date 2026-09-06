---
name: MindFS Relay · C 松绿信息板
description: Relay 账号、节点与设备绑定页面的已实现设计系统
colors:
  bg: '#edf1eb'
  paper: '#fbfcf7'
  pine: '#214d3c'
  text: '#21463a'
  muted: '#58714f'
  line: '#d7e1d1'
  input: '#fffefa'
  primary: '#2e6046'
  primary-hover: '#214d3c'
  mint: '#dfe9d6'
  mint-hover: '#cddfbe'
  danger: '#934838'
  danger-bg: '#f5e8df'
  on-pine: '#f2f4e7'
  on-primary: '#f9fcf2'
  button-text: '#42663c'
  button-hover: '#e8efdf'
  button-active: '#dae5d0'
  danger-fill: '#914839'
  danger-fill-hover: '#783a2d'
  on-danger: '#fff6ef'
  disabled-bg: '#d8e3d0'
  disabled-text: '#677d5e'
  online: '#426f45'
  online-bg: '#e9f0de'
  offline: '#5b7051'
  offline-bg: '#f1f4ec'
  success: '#3d713e'
  success-bg: '#e8efdc'
  focus: '#547d52'
  input-focus: '#739362'
typography:
  display:
    fontFamily: '"SF Pro Display", "PingFang SC", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    fontSize: 48px
    fontWeight: 650
    lineHeight: 1.3
    letterSpacing: -.04em
  headline:
    fontFamily: '"SF Pro Display", "PingFang SC", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    fontSize: 32px
    fontWeight: 650
    lineHeight: 1.3
    letterSpacing: -.03em
  title:
    fontFamily: '"SF Pro Display", "PingFang SC", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    fontSize: 24px
    fontWeight: 600
    lineHeight: 1.4
    letterSpacing: -.025em
  node-title:
    fontFamily: '"SF Pro Display", "PingFang SC", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    fontSize: 22px
    fontWeight: 550
    lineHeight: 1.4
    letterSpacing: -.02em
  body:
    fontFamily: '"SF Pro Display", "PingFang SC", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    fontSize: 15px
    fontWeight: 400
    lineHeight: 1.6
  label:
    fontFamily: '"SF Pro Display", "PingFang SC", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    fontSize: 13px
    fontWeight: 550
    lineHeight: 1.5
  supporting:
    fontFamily: '"SF Pro Display", "PingFang SC", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    fontSize: 13px
    lineHeight: 1.8
  mono:
    fontFamily: ui-monospace, SFMono-Regular, Consolas, monospace
    fontSize: 11px
    lineHeight: 1.7
rounded:
  status: 4px
  action: 5px
  control: 6px
  menu: 8px
  panel: 12px
  dialog-mobile: 14px
  circle: 50%
spacing:
  field: 8px
  control: 12px
  action: 16px
  section: 24px
  panel: 32px
components:
  button-secondary:
    textColor: '{colors.button-text}'
    typography: '{typography.label}'
    rounded: '{rounded.control}'
    padding: 10px 16px
  button-primary:
    backgroundColor: '{colors.primary}'
    textColor: '{colors.on-primary}'
    typography: '{typography.label}'
    rounded: '{rounded.control}'
    padding: 10px 16px
  button-primary-hover:
    backgroundColor: '{colors.primary-hover}'
  button-quiet:
    textColor: '{colors.muted}'
    typography: '{typography.label}'
    rounded: '{rounded.control}'
    padding: 10px 16px
  button-danger:
    textColor: '{colors.danger}'
    typography: '{typography.label}'
    rounded: '{rounded.control}'
    padding: 10px 16px
  button-danger-fill:
    backgroundColor: '{colors.danger-fill}'
    textColor: '{colors.on-danger}'
    typography: '{typography.label}'
    rounded: '{rounded.control}'
    padding: 10px 16px
  button-open:
    backgroundColor: '{colors.mint}'
    textColor: '#315c3b'
    rounded: '{rounded.action}'
    padding: 8px 16px
  button-code:
    backgroundColor: '#e6eddd'
    textColor: '{colors.button-text}'
    rounded: '{rounded.control}'
    padding: 10px 12px
  input:
    backgroundColor: '{colors.input}'
    textColor: '#294c32'
    rounded: '{rounded.control}'
    padding: 11px 13px
    width: 100%
  account-mode:
    textColor: '{colors.muted}'
    padding: 0 0 12px
  status-online:
    backgroundColor: '{colors.online-bg}'
    textColor: '{colors.online}'
    rounded: '{rounded.status}'
    padding: 5px 9px
    width: 67px
  status-offline:
    backgroundColor: '{colors.offline-bg}'
    textColor: '{colors.offline}'
    rounded: '{rounded.status}'
    padding: 5px 9px
    width: 67px
  panel:
    backgroundColor: '{colors.paper}'
    rounded: '{rounded.panel}'
  node-row:
    padding: 25px 14px
  dialog:
    backgroundColor: '{colors.paper}'
    textColor: '{colors.text}'
    rounded: '{rounded.panel}'
    padding: 28px 30px 30px
    width: min(460px, calc(100% - 32px))
---

# Design System: MindFS Relay · C 松绿信息板

## Overview

**Creative North Star: "C 松绿信息板"**

松绿标题区、冷白内容区和浅绿页面底构成稳定的任务界面。名称、状态、输入与操作保持清楚的层级；节点使用连续行，电脑便于按列比较，手机便于逐项操作。

本系统仅覆盖 `cloud/app/pages/` 下的 Relay 页面与共享组件。规范由 `shared.html`、`login.html`、`nodes.html`、`bind.html` 的当前实现提取；上方 token 是数值依据，未建立全仓库或上游 Web 的样式约定。

**Key Characteristics:**

- 松绿标题区与冷白内容形成明确分区。
- 节点列表保持连续行，电脑和手机同等重视。
- 轻边框、低圆角与线性图标；状态同时使用文字和颜色。
- 菜单承载次要操作，任务反馈留在当前表单或列表附近。

## Colors

同一组偏绿中性色支撑背景、文字与分隔；松绿负责标题和主要操作，砖红仅表达错误与破坏性操作。

### Primary

`pine` 用于标题区，`primary` 与 `primary-hover` 用于主按钮；`on-pine`、`on-primary` 分别承载其浅色文字。`mint` 与 `mint-hover` 用于节点打开按钮。`focus`、`input-focus` 表示可见焦点。

### Neutral

`bg` 是页面底，`paper` 是内容与弹层，`input` 是输入区；`text`、`muted` 和 `line` 分别承担正文、辅助信息和分隔。按钮的常态、悬停、按下与禁用色按同名前缀 token 使用。

在线与离线各有文字和底色，徽标同时保留圆点与中文状态。成功反馈使用 `success` 系列；错误和拒绝使用 `danger` 系列，最终删除确认使用 `danger-fill` 系列。在线文字采用已修正的 `online` token。

## Typography

全界面沿用系统无衬线字体栈；节点 ID 使用独立等宽栈。没有下载字体或展示性字族。

`display` 用于节点页主标题，`headline` 用于账号与绑定页标题，`title` 用于二级标题，`node-title` 用于设备名称，`body` 为正文，`label` 为按钮，`supporting` 为辅助说明，`mono` 为 ID。字段标签另用中等字重（500）；计数和时间使用等宽数字。

手机标题和设备字号随布局缩小，输入字体保持易读（16px）。辅助信息按位置使用（10–14px）；不把所有小字统一为同一字号。

## Layout

顶部栏桌面高（96px）、水平内边距（64px）。账号容器宽（`min(490px, 100%)`），绑定容器宽（`min(570px, 100%)`），两者含左右内边距（20px）。节点工作区最大宽（1260px），左右内边距（24px），顶部间距（42px）。面板正文桌面内边距为（28px 32px 32px）。

节点标题与冷白列表上下连接。桌面列表四列为（`minmax(0, 1fr) 110px 140px 104px`），列距（24px）；节点名、状态、最近连接、打开操作逐列对齐。表单垂直间距（19px），验证码输入与发送按钮同行。

| 最大视口宽度 | 已实现变化 |
| --- | --- |
| 950px | 连接示意缩至（270px）；列表列宽改为（`minmax(0, 1fr) 82px 94px 90px`）、列距（16px），名称字号（19px）。 |
| 760px | 隐藏连接示意；顶部栏左右内边距（24px），账号显示截断上限缩至（20ch）。 |
| 600px | 顶部栏高（76px）；隐藏顶部辅助说明、行内邮箱与“账号”文字，菜单内保留完整邮箱。主内容顶部间距（24px）。 |

手机账号正文内边距（24px），标题字号（29px），注册表单进一步收紧间距。节点标题字号（36px），工作区左右留白（20px），列表隐藏列标题。每个设备第一行放图标、名称、ID 和菜单，第二行放状态、时间与打开操作；名称字号（17px），长名称与 ID 可断行。

弹窗桌面居中；手机改为靠底面板，底部间距（`max(16px, env(safe-area-inset-bottom))`），按钮等宽分配。弹窗最大高（`calc(100dvh - 32px)`），内容超高时内部滚动。

## Elevation & Depth

主要面板没有阴影，依靠松绿、冷白和细分隔建立层次。浮出菜单采用唯一常用阴影（`0 8px 28px -8px #18372355`），层级（z-index: 5）。原生模态弹窗使用深绿遮罩（`rgb(17 43 29 / .56)`），自身没有显式阴影。

## Shapes

面板、控件、菜单、状态徽标分别使用上方对应圆角 token。桌面弹窗沿用面板圆角，手机弹窗使用 `dialog-mobile`；头像、绑定状态图形与步骤编号使用圆形。容器边界以色块为主，控件和列表分隔使用细线（1px）。图标为内联 SVG 的圆端线条，常规尺寸（22px）、笔画（1.6），按钮中缩至（18px）。

## Components

### Buttons

主按钮用松绿实底；次按钮透明底配细边框；轻按钮去掉边框颜色。拒绝绑定用砖红文字边框，删除确认用砖红实底。普通按钮最小高（44px），主按钮最小高（48px）；整行表单提交占满宽度。节点打开按钮用浅绿，验证码发送按钮与输入框并排。

悬停改变底色与边框，基础按下态加深底色；键盘焦点使用外描边（2px）与偏移（3px）。底色和边框过渡为（160ms ease-out）。提交时显示进行中文字、设置 `aria-busy` 并禁用按钮；验证码重发倒计时采用服务端时间，缺省（60 秒）。

### Inputs / Fields

浅色输入区使用细边框和控件圆角，最小高（48px）。输入框焦点描边（2px）、偏移（2px）；`aria-invalid="true"` 使用砖红边框。标签始终可见，占位文字只提供示例。错误提示使用 `role="alert"`，成功提示使用 `role="status"`；空提示不占位。

### Navigation

品牌链接返回节点页。账号入口使用三等分切换按钮及底线表示当前项，以 `aria-pressed` 标记选中状态，切换时更新表单和标题，并复用已填邮箱。它是按钮式导航，没有实现方向键标签页交互。

账号与节点菜单使用原生 `details` / `summary`。账号菜单显示邮箱、修改密码、退出；节点菜单提供重命名和删除。菜单右对齐，账号面板宽（214px）、节点面板宽（168px），手机节点菜单宽（145px）。点击外部或其他菜单收起当前菜单；Escape 收起菜单并将焦点放回触发器。

### Cards / Containers

账号与绑定页使用同一面板壳：松绿标题区接冷白正文，不额外叠卡。节点页沿用相同材质，但列表形成连续横向行。初次加载用静态骨架占位；空列表说明如何从设备发起绑定，并保留刷新操作。

### Node Rows & Status

每行包含设备图标、名称、ID、操作菜单、文字状态、最近连接时间与打开入口。在线名称和“打开”均可进入节点；离线入口保留可识别位置和 `aria-disabled`，点击时显示恢复说明并阻止导航。最近连接默认使用相对时间，悬停标题提供完整时间。

列表每（15 秒）刷新，并复用已有行以保留键盘焦点。更新失败保留上次内容，明确说明数据未更新，并提供重新加载。状态徽标使用细边框、小圆角、圆点和文字；手机缩小尺寸，保留语义。

### Dialogs

重命名、删除、修改密码使用原生 `showModal()`，关联标题与说明。重命名打开后选中当前名称；修改密码焦点进入当前密码；删除默认聚焦取消，并展示设备名称、ID 与断开后需重新绑定的后果。

关闭按钮、取消或 Escape 可关闭弹窗；未实现点击遮罩关闭。提交期间禁用关闭按钮并阻止 Escape，失败时保留表单及内联错误。关闭后清理表单与错误并恢复触发器焦点；删除成功后将焦点移到刷新按钮。入场位移（9px），时长（180ms），曲线（`cubic-bezier(.16, 1, .3, 1)`）；减少动态效果偏好会关闭过渡与动画。

### Binding Progress

有序步骤显示“设备发起 → 确认绑定 → 连接节点”，当前步骤使用 `aria-current="step"`；等待和连接阶段解释当前状态。确认后每（1.5 秒）检查连接，在线后才显示“打开节点”；等待超过（15 秒）补充继续运行设备的说明。过期、拒绝和网络故障分别显示对应恢复动作。

## Do's and Don'ts

### Do:

- Do 沿用松绿标题区、冷白内容与浅绿页底的层次。
- Do 在窄屏保留名称、状态、最近连接和打开操作，允许长名称与 ID 换行。
- Do 保留可见键盘焦点、明确字段标签、提交反馈和关闭后的焦点恢复。

### Don't:

- Don't 将本范围的节点列表改为独立设备卡片网格。
- Don't 只用圆点或颜色表达在线、离线和错误。
- Don't 让离线节点直接导航，或让异步刷新丢弃正在使用的节点行焦点。

机器可读扩展位于 `.impeccable/design.json`。其中色阶为面板预览合成值，不是新增的生产 token；组件片段来自当前共享样式，交互行为以这里描述的页面实现为准。
