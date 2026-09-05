---
name: ProjectBoard
description: 以 Kaneo 应用框架为参考的本地工作空间
colors:
  primary: "#252525"
  background: "#ffffff"
  sidebar: "#f7f7f8"
  border: "#e8e8eb"
  text: "#27272a"
  muted: "#71717a"
typography:
  micro:
    fontSize: "10px"
  mobile-title:
    fontSize: "21px"
  body:
    fontFamily: "Segoe UI, Microsoft YaHei, sans-serif"
    fontSize: "13px"
    lineHeight: 1.55
rounded:
  control: "6px"
  workspace: "12px"
spacing:
  compact: "8px"
  standard: "16px"
  panel: "24px"
---

# Design System: ProjectBoard

## Overview

以 Kaneo 官方应用演示为界面依据：窄侧栏、紧凑的顶部路径与页签、独立筛选栏、铺满窗口的工作区域。视图直接从任务或知识内容开始。

## Colors

采用黑白中性色。主按钮和当前标签通过深浅对比表达；知识引用使用少量蓝色，任务语义状态使用有限的辅助色。

## Typography

全界面采用系统无衬线字体。导航与控制为 12–13px，任务标题为 13px，节点标题为 22px。点分地址使用等宽字体以便辨认路径。

## Layout

桌面侧栏宽 232px，外部工作区占其余空间，外缘保留 8px。工作区顶部 48px：左侧路径、中部项目页签、右侧操作。视图工具栏 48px。任务列与知识树各自滚动，浏览器窗口不随节点内容无限增长。

知识库左栏 248px，中间为内容编辑区域，右栏 250px 为标签、关联和位置操作。1100px 以下属性栏放入内容区下方，760px 以下全局侧栏收起，知识树可以展开或收起。

## Elevation & Depth

边界与背景层级划分主要工作区。阴影用于模态框和手机上展开的知识树覆盖层，具体阴影记录在设计 sidecar。

## Shapes

工作区圆角 12px，按钮 6px，任务卡片 8px。知识树和属性区以直线分隔。

## Components

侧栏顶部工作空间与管理员入口，下面是搜索、工作区导航和项目列表。项目页签与路径处于同一条导航行。工具栏负责筛选与创建动作。看板列头固定，卡片包含编号、标题、标签和必要状态。

知识树是持续存在的导航区；节点标题和正文是主要工作区；标签与关联是右侧属性。侧栏、内容和属性使用独立滚动。
