<?php require_once 'config/config.php'; require_once 'config/auth.php'; requireLogin(); ?>
<!DOCTYPE html>
<html lang="zh">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>星记</title>
    <meta name="csrf-token" content="" id="csrfMeta">
    <link href="bootstrap-icons.min.css" rel="stylesheet">
    <link href="css/style.css?v=24" rel="stylesheet">
</head>
<body>
    <!-- 星空背景 Canvas -->
    <canvas id="starfield"></canvas>

    <!-- 主应用层 -->
    <div id="app">

        <!-- 顶部导航 -->
        <nav id="nav">
            <span class="nav-brand">星记</span>
            <div class="nav-tabs">
                <button class="nav-tab active" data-view="today">
                    <i class="bi bi-moon-stars"></i> 今日星空
                </button>
                <button class="nav-tab" data-view="history">
                    <i class="bi bi-clock-history"></i> 星史台历
                </button>
            </div>
            <button class="theme-toggle" id="themeToggle" title="切换主题"><i class="bi bi-moon-stars-fill"></i></button>
            <button class="btn-logout" id="btnLogout" title="登出"><i class="bi bi-box-arrow-right"></i></button>
        </nav>

        <!-- 今日星空视图 -->
        <div id="today-view" class="view">
            <!-- 移动端 Tab 切换栏 -->
            <div class="mobile-tabs" id="mobileTabs">
                <button class="mobile-tab active" data-mobile-tab="timeline">
                    <i class="bi bi-clock"></i> 时间轴
                </button>
                <button class="mobile-tab" data-mobile-tab="constellation">
                    <i class="bi bi-stars"></i> 星座
                </button>
            </div>
            <!-- 时间轴（左侧） -->
            <div id="today-zone">
                <div id="timeline">
                    <div class="timeline-header">
                        <i class="bi bi-clock"></i> 今日轨道
                    </div>
                    <div class="timeline-body" id="timelineBody"></div>
                </div>
            </div>

            <!-- 星空总览（右侧，全部任务） -->
            <div id="constellation-zone" class="mobile-hidden"></div>

            <!-- 类型图例（固定底部右侧） -->
            <div class="constellation-legend" id="constellationLegend"></div>
        </div>

        <!-- 星史台历视图 -->
        <div id="history-view" class="view hidden">
            <div id="history-content">
                <div class="calendar-nav">
                    <button class="cal-nav-btn" id="calPrev"><i class="bi bi-chevron-left"></i></button>
                    <span class="cal-month-label" id="calMonthLabel"></span>
                    <button class="cal-nav-btn" id="calNext"><i class="bi bi-chevron-right"></i></button>
                    <button class="cal-nav-btn cal-today-btn" id="calToday">今天</button>
                    <button class="cal-nav-btn cal-export-btn desktop-only" id="btnExportCSV" title="导出已完成任务为 CSV"><i class="bi bi-download"></i> 导出</button>
                </div>
                <div class="calendar-weekdays">
                    <span>一</span><span>二</span><span>三</span><span>四</span><span>五</span><span>六</span><span>日</span>
                </div>
                <div class="calendar-grid" id="calendarGrid"></div>
            </div>
        </div>

        <!-- 日历弹出浮层 -->
        <div id="calPopup" class="cal-popup hidden">
            <div class="cal-popup-backdrop"></div>
            <div class="cal-popup-card">
                <div class="cal-popup-header">
                    <h4 id="calPopupTitle"></h4>
                    <button class="cal-popup-close" id="calPopupClose"><i class="bi bi-x-lg"></i></button>
                </div>
                <div class="cal-popup-body" id="calPopupBody"></div>
            </div>
        </div>

        <!-- 确认弹窗 -->
        <div id="confirmDialog" class="cal-popup hidden">
            <div class="cal-popup-backdrop"></div>
            <div class="cal-popup-card" style="max-width: 360px;">
                <div class="cal-popup-header">
                    <h4 id="confirmTitle">确认</h4>
                </div>
                <div class="cal-popup-body">
                    <p id="confirmMessage" style="margin: 0 0 16px; line-height: 1.6;"></p>
                    <div style="display: flex; gap: 10px; justify-content: flex-end;">
                        <button class="btn-delete" id="confirmCancel" style="flex: 0 0 auto; padding: 8px 16px;">取消</button>
                        <button class="btn-complete" id="confirmOk" style="flex: 0 0 auto; padding: 8px 16px;">确定</button>
                    </div>
                </div>
            </div>
        </div>

        <!-- 导出弹窗（仅电脑端） -->
        <div id="exportDialog" class="export-dialog hidden">
            <div class="export-dialog-card">
                <h4><i class="bi bi-download"></i> 导出已完成任务</h4>
                <div class="export-dialog-row">
                    <label>起始月</label>
                    <input type="month" id="exportFrom">
                </div>
                <div class="export-dialog-row">
                    <label>结束月</label>
                    <input type="month" id="exportTo">
                </div>
                <div class="export-dialog-actions">
                    <button class="btn-cancel" id="exportCancel">取消</button>
                    <button class="btn-export" id="exportConfirm">导出 CSV</button>
                </div>
            </div>
        </div>

        <!-- 倾倒输入区 -->
        <div id="input-nebula">
            <form id="quickAdd" autocomplete="off">
                <div class="input-row">
                    <input type="text" id="quickInput" placeholder="写下你脑子里的事..." autofocus>
                    <button type="submit" class="btn-add"><i class="bi bi-plus-lg"></i></button>
                </div>
                <div class="input-options">
                    <div class="type-selector">
                        <button type="button" class="type-dot active" data-type="self" title="自我时间" style="--dot-color: #9370db"></button>
                        <button type="button" class="type-dot" data-type="family" title="家庭" style="--dot-color: #9acd32"></button>
                        <button type="button" class="type-dot" data-type="money" title="赚钱" style="--dot-color: #ff6347"></button>
                        <button type="button" class="type-dot" data-type="sport" title="运动" style="--dot-color: #ffd700"></button>
                        <button type="button" class="type-dot" data-type="love" title="爱情" style="--dot-color: #ff69b4"></button>
                        <button type="button" class="type-dot" data-type="study" title="学习" style="--dot-color: #4169e1"></button>
                    </div>
                    <div class="date-options">
                        <div class="date-selector" id="dateSelector">
                            <button type="button" class="date-chip" data-date="today">今天</button>
                            <div class="date-wrap">
                                <input type="text" id="inputStartDateText" class="date-input date-display" placeholder="" readonly>
                                <input type="date" id="inputStartDate" class="date-input date-real">
                            </div>
                            <span class="date-sep">→</span>
                            <div class="date-wrap">
                                <input type="text" id="inputDueDateText" class="date-input date-display" placeholder="" readonly>
                                <input type="date" id="inputDueDate" class="date-input date-real">
                            </div>
                        </div>
                    </div>
                </div>
            </form>
        </div>

        <!-- 详情面板 -->
        <div id="detail-panel">
            <div class="detail-header">
                <div class="detail-type-dot" id="detailTypeDot"></div>
                <h3 id="detailTitle"></h3>
                <input type="text" id="detailTitleInput" class="detail-title-input" style="display:none">
                <button class="detail-close" id="detailClose"><i class="bi bi-x-lg"></i></button>
            </div>
            <div class="detail-type-edit" id="detailTypeEdit">
                <span class="detail-edit-label"><i class="bi bi-tag"></i> 类型</span>
                <div class="detail-type-dots" id="detailTypeDots"></div>
            </div>
            <div class="detail-progress" id="detailProgressBlock">
                <div class="progress-bar-container" id="progressBarClick">
                    <div class="progress-bar-fill" id="detailProgressFill"></div>
                </div>
                <span class="progress-text" id="detailProgressText">0%</span>
                <input type="range" min="0" max="100" value="0" class="progress-slider hidden" id="progressSlider">
            </div>
            <div class="detail-meta" id="detailMeta"></div>
            <div class="detail-date-edit" id="detailDateEdit">
                <label class="date-edit-row">
                    <span class="date-edit-label"><i class="bi bi-hourglass-split"></i> 开始</span>
                    <input type="date" id="detailStartDate" class="date-input">
                </label>
                <label class="date-edit-row">
                    <span class="date-edit-label"><i class="bi bi-calendar-event"></i> 截止</span>
                    <input type="date" id="detailDueDate" class="date-input">
                </label>
            </div>
            <div class="detail-children" id="detailChildren"></div>
            <div class="detail-actions">
                <button class="btn-complete" id="btnComplete"><i class="bi bi-check2-circle"></i> 完成</button>
                <button class="btn-complete-date" id="btnCompleteDate" title="完成日期">
                    <i class="bi bi-calendar-check"></i>
                    <span id="completeDateLabel">今天</span>
                </button>
                <input type="date" id="completedDate" class="completed-date-hidden">
                <button class="btn-delete" id="btnDelete"><i class="bi bi-trash3"></i> 删除</button>
            </div>
            <div class="detail-add-child">
                <form id="addChildForm" autocomplete="off">
                    <input type="text" id="addChildInput" placeholder="添加子任务...">
                    <button type="submit"><i class="bi bi-plus"></i></button>
                </form>
            </div>
        </div>

        <!-- 遮罩 -->
        <div id="overlay" class="hidden"></div>
    </div>

    <script src="js/starfield.js?v=20"></script>
    <script src="js/main.js?v=20"></script>
</body>
</html>
