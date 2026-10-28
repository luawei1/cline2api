package main

const adminHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Cline 代理管理面板</title>
<style>
:root{
  color-scheme:light;
  --bg:#f5f5f7;--surface:#ffffff;--surface2:#fbfbfd;
  --border:#d2d2d7;--border2:#e8e8ed;
  /* 辅助文字提高一档对比度，保证小字号可读（原 text3 #aeaeb2 对比度不足） */
  --text:#1d1d1f;--text2:#6e6e73;--text3:#86868b;
  --accent:#007aff;--accent-hover:#0066d6;--accent-soft:#e8f0ff;
  --green:#34c759;--green-soft:#e8f9ed;
  --red:#ff3b30;--red-soft:#ffefee;
  --yellow:#ff9500;--yellow-soft:#fff5e6;
  --shadow-sm:0 1px 2px rgba(0,0,0,0.04),0 1px 3px rgba(0,0,0,0.06);
  --shadow-md:0 4px 12px rgba(0,0,0,0.06),0 1px 3px rgba(0,0,0,0.04);
  --shadow-lg:0 12px 32px rgba(0,0,0,0.08),0 2px 8px rgba(0,0,0,0.04);
  --radius:16px;--radius-sm:10px;--radius-xs:8px;
  --ease:cubic-bezier(0.4,0,0.2,1);
  --blue:#007aff;
  /* ===== 设计令牌：间距 / 字号 / 控件尺寸 / 焦点环 ===== */
  --sp-1:4px;--sp-2:8px;--sp-3:12px;--sp-4:16px;--sp-5:20px;--sp-6:24px;--sp-7:32px;--sp-8:40px;
  --fs-2xs:10px;--fs-xs:11px;--fs-sm:12px;--fs-base:13px;--fs-md:14px;--fs-lg:15px;--fs-xl:20px;--fs-2xl:28px;--fs-3xl:32px;
  --control-h:38px;--control-h-sm:30px;
  --ring:0 0 0 3px var(--accent-soft);
  --radius-lg:16px;--radius-pill:999px;
  --overlay-bg:rgba(15,23,42,0.45);
}
*{margin:0;padding:0;box-sizing:border-box}
html{-webkit-text-size-adjust:100%}
body{font-family:-apple-system,BlinkMacSystemFont,'SF Pro Display','SF Pro Text','Segoe UI','Noto Sans',Helvetica,Arial,sans-serif;background:var(--bg);color:var(--text);font-size:15px;line-height:1.5;-webkit-font-smoothing:antialiased;text-rendering:optimizeLegibility}
.layout{display:flex;min-height:100vh}

/* ===== Sidebar ===== */
.sidebar{width:260px;background:var(--surface);border-right:1px solid var(--border2);padding:0;flex-shrink:0;display:flex;flex-direction:column;position:sticky;top:0;height:100vh;overflow:hidden}
.sidebar-header{padding:20px 20px 16px;border-bottom:1px solid var(--border2);flex-shrink:0}
.sidebar-nav{flex:1 1 auto;min-height:0;overflow-y:auto;overflow-x:hidden;overscroll-behavior:contain;padding-bottom:6px}
.sidebar-header .brand{display:flex;align-items:center;gap:10px}
.sidebar-header .logo{width:32px;height:32px;border-radius:8px;background:linear-gradient(135deg,#007aff,#5856d6);display:flex;align-items:center;justify-content:center;flex-shrink:0;box-shadow:var(--shadow-sm)}
.sidebar-header .logo svg{width:18px;height:18px;color:#fff}
.sidebar-header .brand-name{font-size:15px;font-weight:600;color:var(--text);letter-spacing:-0.2px}
.sidebar-header .brand-sub{font-size:12px;color:var(--text2);margin-top:2px}
.nav-section{padding:16px 12px 8px}
.nav-section-label{font-size:11px;font-weight:600;color:var(--text3);text-transform:uppercase;letter-spacing:0.5px;padding:0 8px 8px}
.nav-item{display:flex;align-items:center;gap:12px;padding:9px 12px;cursor:pointer;color:var(--text2);transition:all 0.18s var(--ease);border-radius:var(--radius-xs);margin-bottom:2px;font-size:14px;font-weight:500}
.nav-item:hover{color:var(--text);background:var(--surface2)}
.nav-item.active{color:var(--accent);background:var(--accent-soft)}
.nav-item svg{width:20px;height:20px;flex-shrink:0}
.nav-item .nav-label{flex:1}
.nav-item.dragging{opacity:0.5}
.nav-item.drop-target{box-shadow:inset 0 2px 0 var(--accent)}
.sidebar-footer{padding:16px 20px;border-top:1px solid var(--border2);font-size:12px;color:var(--text2);flex-shrink:0}
.sidebar-lang{padding:0 20px 16px;border-top:1px solid var(--border2);flex-shrink:0}
.sidebar-lang .lang-switch{margin-top:12px}
.sidebar-footer a{color:var(--accent);text-decoration:none}
.sidebar-footer a:hover{text-decoration:underline}
.lang-switch{display:flex;gap:4px;margin-top:10px}
.lang-switch button{flex:1;padding:5px 0;border:1px solid var(--border2);border-radius:8px;background:var(--surface);color:var(--text2);font-size:11px;font-weight:600;cursor:pointer;transition:all 0.18s var(--ease)}
.lang-switch button:hover{color:var(--text);border-color:var(--text3)}
.lang-switch button.active{color:var(--accent);background:var(--accent-soft);border-color:transparent}
.sync-btn{display:inline-flex;align-items:center;gap:6px;padding:6px 14px;border:1px solid var(--border2);border-radius:var(--radius-sm);background:var(--surface);color:var(--accent);cursor:pointer;font-size:13px;font-weight:500;transition:all 0.18s var(--ease);white-space:nowrap}
.sync-btn:hover{background:var(--accent-soft);border-color:var(--accent)}
.sync-btn:disabled{opacity:0.6;cursor:wait}

/* ===== Main ===== */
.main{flex:1;min-width:0;padding:36px clamp(24px,4vw,64px) 64px;overflow-y:auto}
.content-shell{width:min(100%,1440px);margin:0 auto}
.large-title{font-size:32px;font-weight:700;letter-spacing:0;color:var(--text);margin-bottom:4px;text-wrap:pretty}
.large-subtitle{font-size:15px;color:var(--text2);margin-bottom:28px}
.page-header{display:flex;justify-content:space-between;align-items:flex-end;gap:20px;margin-bottom:28px}
.page-header h2{font-size:28px;font-weight:700;letter-spacing:0}

/* ===== Dashboard ===== */
.metric-section{margin-bottom:26px}
.metric-heading{font-size:13px;font-weight:600;color:var(--text2);margin:0 0 10px 2px}
.cards{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:14px}
.cards.tokens{grid-template-columns:repeat(4,minmax(0,1fr))}
.card{min-width:0;background:var(--surface);border:1px solid var(--border2);border-radius:12px;padding:18px;box-shadow:var(--shadow-sm);transition:transform 0.2s var(--ease),box-shadow 0.2s var(--ease)}
.card:hover{transform:translateY(-1px);box-shadow:var(--shadow-md)}
.card .card-icon{width:34px;height:34px;border-radius:10px;display:flex;align-items:center;justify-content:center;margin-bottom:12px}
.card .card-icon svg{width:18px;height:18px}
.card .num{overflow:hidden;text-overflow:ellipsis;font-size:30px;font-weight:700;letter-spacing:0;line-height:1.1;font-variant-numeric:tabular-nums}
.card .label{font-size:13px;color:var(--text2);margin-top:5px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.card.blue .card-icon{background:var(--accent-soft)}.card.blue .card-icon svg{color:var(--blue)}.card.blue .num{color:var(--blue)}
.card.green .card-icon{background:var(--green-soft)}.card.green .card-icon svg{color:var(--green)}.card.green .num{color:var(--green)}
.card.yellow .card-icon{background:var(--yellow-soft)}.card.yellow .card-icon svg{color:var(--yellow)}.card.yellow .num{color:var(--yellow)}
.card.red .card-icon{background:var(--red-soft)}.card.red .card-icon svg{color:var(--red)}.card.red .num{color:var(--red)}

/* ===== Section / Grouped ===== */
.section{background:var(--surface);border:1px solid var(--border2);border-radius:var(--radius);margin-bottom:var(--sp-5);overflow:hidden;box-shadow:var(--shadow-sm)}
.section-title{padding:var(--sp-4) var(--sp-5) var(--sp-2);font-weight:600;font-size:var(--fs-lg);line-height:1.35;display:flex;align-items:center;gap:var(--sp-2);color:var(--text)}
.section-title svg{width:18px;height:18px;color:var(--text2);flex-shrink:0}
.section-desc{padding:0 var(--sp-5) var(--sp-3);font-size:var(--fs-base);line-height:1.6;color:var(--text2)}
.section-body{padding:var(--sp-4) var(--sp-5)}
.section-body.flush{padding:0;overflow-x:auto;-webkit-overflow-scrolling:touch}
.section-body.xscroll{overflow-x:auto;-webkit-overflow-scrolling:touch}

/* ===== Tabs (segmented) ===== */
.tabs{display:flex;border-bottom:1px solid var(--border2);padding:0 20px;gap:4px}
.tab{padding:12px 18px;cursor:pointer;color:var(--text2);border-bottom:2px solid transparent;font-size:14px;font-weight:500;transition:all 0.18s var(--ease)}
.tab:hover{color:var(--text)}
.tab.active{color:var(--accent);border-bottom-color:var(--accent)}
.tab-content{display:none;padding:20px}
.tab-content.active{display:block}

/* ===== Table ===== */
table{width:100%;border-collapse:collapse;table-layout:fixed}
.model-subtable{table-layout:auto}
.model-subtable td{padding:7px 12px;text-align:left;white-space:nowrap}
.model-subtable td:nth-child(n+3){text-align:right;font-variant-numeric:tabular-nums}
th,td{text-align:left;padding:10px var(--sp-3);border-bottom:1px solid var(--border2);font-size:var(--fs-base);vertical-align:middle;overflow-wrap:break-word}
.section-body.flush{overflow-x:auto;-webkit-overflow-scrolling:touch}
th{color:var(--text2);font-weight:600;font-size:var(--fs-xs);text-transform:uppercase;letter-spacing:0.4px;white-space:nowrap;background:var(--surface)}
td.mono{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
tbody tr:last-child td{border-bottom:none}
tbody tr{transition:background 0.15s var(--ease)}
tbody tr:hover{background:var(--surface2)}
/* 账号表格列宽：1 勾选 / 2 序号 / 3 邮箱(自适应省略) / 4 状态 / 5-9 数字右对齐 / 10-11 时间不折行 / 12 操作 */
.account-table th:nth-child(1),.account-table td:nth-child(1){width:36px;min-width:36px;text-align:center}
.account-table th:nth-child(2),.account-table td:nth-child(2){width:44px;min-width:44px;color:var(--text3)}
.account-table td:nth-child(3){overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.account-table th:nth-child(4),.account-table td:nth-child(4){width:9%}
.account-table th:nth-child(5),.account-table td:nth-child(5),.account-table th:nth-child(6),.account-table td:nth-child(6),.account-table th:nth-child(7),.account-table td:nth-child(7),.account-table th:nth-child(8),.account-table td:nth-child(8),.account-table th:nth-child(9),.account-table td:nth-child(9){width:7%;text-align:right;font-variant-numeric:tabular-nums}
.account-table th:nth-child(10),.account-table td:nth-child(10),.account-table th:nth-child(11),.account-table td:nth-child(11){width:10%;color:var(--text2)}
.account-table th:last-child,.account-table td:last-child{width:172px;min-width:172px;text-align:right;white-space:nowrap}
.account-table input[type="checkbox"]{width:16px;height:16px;accent-color:var(--accent);cursor:pointer;vertical-align:middle}
.account-table td:last-child .btn{width:32px;padding-left:0;padding-right:0;justify-content:center}
.account-email{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--text);font-weight:500}
.account-cards{display:none}
.log-cards{display:none}
.log-table th:first-child,.log-table td:first-child{width:14%;white-space:nowrap;color:var(--text2)}
.log-table th:nth-child(2),.log-table td:nth-child(2){width:16%}
.log-table th:nth-child(3),.log-table td:nth-child(3){width:7%}
.log-table th:nth-child(4),.log-table td:nth-child(4){width:15%}
.log-table th:nth-child(5),.log-table td:nth-child(5),.log-table th:nth-child(6),.log-table td:nth-child(6),.log-table th:nth-child(7),.log-table td:nth-child(7),.log-table th:nth-child(8),.log-table td:nth-child(8){width:6%;text-align:right;font-variant-numeric:tabular-nums}
.log-table th:nth-child(9),.log-table td:nth-child(9),.log-table th:nth-child(10),.log-table td:nth-child(10),.log-table th:nth-child(11),.log-table td:nth-child(11){width:6%;text-align:right;font-variant-numeric:tabular-nums}
.log-table th:last-child,.log-table td:last-child{width:6%;text-align:right;white-space:nowrap}
.log-status{display:inline-flex;align-items:center;gap:5px;padding:3px 10px;border-radius:12px;font-size:11px;font-weight:600}
.log-status.ok{background:var(--green-soft);color:var(--green)}
.log-status.fail{background:var(--red-soft);color:var(--red)}
.log-table tbody tr.log-row.has-attempts{cursor:pointer}
.log-table tbody tr.log-row.has-attempts:hover{background:var(--surface2)}
.log-model-cell{display:flex;flex-direction:column;gap:3px;align-items:flex-start}
.log-combo-badge{display:inline-block;padding:1px 7px;border-radius:6px;font-size:10px;font-weight:500;background:var(--accent-soft);color:var(--accent);white-space:nowrap;border:1px solid transparent}
.log-fallback-badge{display:inline-block;padding:1px 7px;border-radius:6px;font-size:10px;font-weight:500;background:var(--yellow-soft);color:var(--yellow);white-space:nowrap;border:1px solid transparent}
.log-req-model{font-size:10px;color:var(--text3)}
.log-detail-cell{background:var(--surface2);padding:10px 14px !important;text-align:left !important;font-variant-numeric:normal}
.log-attempts{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:6px}
.log-attempts li{display:flex;align-items:center;gap:8px;flex-wrap:wrap;font-size:12px;text-align:left}
.log-attempts .attempt-model{font-family:ui-monospace,monospace;color:var(--text)}
.log-attempts .attempt-meta{color:var(--text3)}
.attempt-outcome{display:inline-block;padding:1px 7px;border-radius:6px;font-size:10px;font-weight:600}
.attempt-outcome.success{background:var(--green-soft);color:var(--green)}
.attempt-outcome.failed{background:var(--red-soft);color:var(--red)}
.attempt-outcome.skipped{background:var(--yellow-soft);color:var(--yellow)}
.attempt-reason{flex-basis:100%;font-size:11px;color:var(--text2);word-break:break-word}
.log-card-attempts{margin-top:8px;border-top:1px solid var(--border2);padding-top:8px}
.log-card-attempts summary{cursor:pointer;font-size:12px;color:var(--accent);font-weight:500}

/* ===== Status badges ===== */
.status{display:inline-flex;align-items:center;gap:5px;padding:3px 10px;border-radius:12px;font-size:12px;font-weight:600}
.status.active{background:var(--green-soft);color:var(--green)}
.status.cooldown{background:var(--yellow-soft);color:var(--yellow)}
.status.expired{background:var(--red-soft);color:var(--red)}
.status-dot{width:6px;height:6px;border-radius:50%;display:inline-block}
.status-dot.active{background:var(--green)}
.status-dot.cooldown{background:var(--yellow)}
.status-dot.expired{background:var(--red)}
.status-cooldown{display:inline-flex;flex-direction:column;align-items:center;gap:0;line-height:1.2}
.status-cooldown .cd-icon{font-size:13px}
.status-cooldown .cd-time{font-size:10px;font-weight:500;opacity:0.8}

/* ===== Buttons ===== */
.btn{display:inline-flex;align-items:center;justify-content:center;gap:6px;padding:var(--sp-2) var(--sp-4);min-height:var(--control-h);border:1px solid var(--border);border-radius:var(--radius-sm);background:var(--surface);color:var(--text);cursor:pointer;font-size:var(--fs-md);font-weight:500;transition:background 0.18s var(--ease),border-color 0.18s var(--ease),transform 0.18s var(--ease),box-shadow 0.18s var(--ease);text-decoration:none;line-height:1.2;white-space:nowrap}
.btn:hover{background:var(--surface2);border-color:var(--text3)}
.btn:active{transform:scale(0.97)}
.btn svg{width:16px;height:16px;flex-shrink:0}
.btn:focus-visible{outline:none;box-shadow:var(--ring);border-color:var(--accent)}
.btn:disabled{opacity:0.5;cursor:not-allowed;transform:none}
.btn-primary{background:var(--accent);border-color:var(--accent);color:#fff}
.btn-primary:hover{background:var(--accent-hover);border-color:var(--accent-hover)}
.btn-success{background:var(--green);border-color:var(--green);color:#fff}
.btn-success:hover{background:#2bb24c;border-color:#2bb24c}
.btn-danger{border-color:var(--red);color:var(--red);background:var(--surface)}
.btn-danger:hover{background:var(--red);color:#fff}
.btn-sm{padding:5px var(--sp-3);min-height:var(--control-h-sm);font-size:var(--fs-base)}
.btn-icon{padding:6px;width:32px;height:32px;min-height:0;justify-content:center}

/* ===== Inputs ===== */
input,textarea,select{width:100%;padding:10px 14px;background:var(--surface);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text);font-size:var(--fs-md);font-family:inherit;transition:border-color 0.18s var(--ease),box-shadow 0.18s var(--ease)}
input:not([type="checkbox"]):not([type="radio"]),select{min-height:var(--control-h)}
input:focus,textarea:focus,select:focus{outline:none;border-color:var(--accent);box-shadow:var(--ring)}
input:disabled,select:disabled,textarea:disabled{background:var(--surface2);color:var(--text2);cursor:not-allowed}
textarea{resize:vertical;min-height:88px;font-family:ui-monospace,'SF Mono','Cascadia Code','Consolas',monospace;font-size:var(--fs-sm);line-height:1.6}
::placeholder{color:var(--text3)}

/* ===== Forms ===== */
.form-row{display:flex;gap:var(--sp-3);align-items:flex-end;margin-bottom:var(--sp-3)}
.form-row .field{flex:1;min-width:0}
.form-row .field label{display:block;font-size:var(--fs-base);color:var(--text2);margin-bottom:6px;font-weight:500}
.form-actions{display:flex;gap:var(--sp-2);margin-top:var(--sp-3);flex-wrap:wrap;align-items:center}

/* ===== Toast ===== */
.toast{position:fixed;top:var(--sp-6);left:50%;transform:translateX(-50%) translateY(-20px);padding:var(--sp-3) var(--sp-5);border-radius:var(--radius-sm);color:#fff;z-index:9999;opacity:0;transition:opacity 0.3s var(--ease),transform 0.3s var(--ease);font-size:var(--fs-md);font-weight:500;max-width:min(420px,calc(100vw - 32px));box-shadow:var(--shadow-lg);display:flex;align-items:center;gap:var(--sp-2)}
.toast.show{opacity:1;transform:translateX(-50%) translateY(0)}
.toast.success{background:var(--green)}
.toast.error{background:var(--red)}
.toast.info{background:var(--accent)}

/* ===== Loading ===== */
.loading{display:inline-block;width:14px;height:14px;border:2px solid var(--text3);border-top-color:var(--accent);border-radius:50%;animation:spin 0.7s linear infinite}
@keyframes spin{to{transform:rotate(360deg)}}

/* ===== Misc ===== */
.empty{padding:40px;text-align:center;color:var(--text2);font-size:14px}
.empty a{color:var(--accent);cursor:pointer;text-decoration:none}
.empty a:hover{text-decoration:underline}
.mono{font-family:ui-monospace,'SF Mono','Cascadia Code','Consolas',monospace;font-size:12px}
.flex{display:flex;align-items:center;gap:8px}
.justify-between{display:flex;justify-content:space-between;align-items:center}
.text-right{text-align:right}
.mt-8{margin-top:8px}
.inline-flex{display:inline-flex;align-items:center;gap:6px}

.key-display{background:var(--surface2);padding:10px 14px;border-radius:var(--radius-sm);border:1px solid var(--border2);font-family:ui-monospace,'SF Mono','Cascadia Code','Consolas',monospace;font-size:12px;word-break:break-all;cursor:pointer;transition:all 0.15s var(--ease);color:var(--text)}
.key-display:hover{background:var(--accent-soft);border-color:var(--accent)}
.copy-icon{cursor:pointer;color:var(--text2);padding:2px 6px;border-radius:4px}
.copy-icon:hover{color:var(--text);background:var(--surface2)}

.empty-state{padding:48px 24px;text-align:center;color:var(--text2)}
.empty-state .icon{width:48px;height:48px;margin:0 auto 14px;display:flex;align-items:center;justify-content:center;border-radius:14px;background:var(--surface2);color:var(--text3)}
.empty-state .icon svg{width:24px;height:24px}

.model-tag{display:inline-block;padding:4px 10px;border-radius:8px;font-size:12px;font-weight:500;background:var(--surface2);color:var(--text2);margin:3px;border:1px solid var(--border2)}
.delisted-badge{display:inline-block;margin-left:6px;padding:1px 6px;border-radius:6px;font-size:10px;color:var(--red);border:1px solid var(--red);font-weight:500}
.model-tag.free{border-color:var(--green);color:var(--green);background:var(--green-soft)}
.model-tag.pass{border-color:var(--yellow);color:var(--yellow);background:var(--yellow-soft)}
.model-item{display:inline-flex;align-items:center;gap:2px;margin:3px}
.model-item .model-tag{margin:0}
.model-group{margin:4px 0 10px}
.model-group-head{display:flex;align-items:center;gap:6px;font-size:12px;font-weight:600;color:var(--text2);cursor:pointer;user-select:none;padding:5px 0}
.model-group-head:hover{color:var(--text)}
.model-group-caret{display:inline-block;width:12px;font-size:11px;color:var(--text3);transition:transform .15s}
.model-group-head.expanded .model-group-caret{transform:rotate(90deg)}
.model-group-count{font-size:11px;color:var(--text3);font-weight:400;background:var(--surface2);border:1px solid var(--border2);border-radius:8px;padding:0 7px;line-height:16px}
.model-group-body{margin:2px 0 4px 18px}
.warn-box{display:flex;align-items:flex-start;gap:8px;margin-top:10px;padding:10px 12px;border-radius:8px;background:var(--yellow-soft);color:var(--yellow);font-size:13px;line-height:1.5;border:1px solid var(--yellow)}
.subtabs{display:flex;gap:2px;border:1px solid var(--border2);border-radius:10px;background:var(--surface2);padding:3px;margin-bottom:18px;width:max-content;max-width:100%;overflow-x:auto}
.subtab{padding:7px 16px;border-radius:8px;cursor:pointer;color:var(--text2);font-size:13px;font-weight:500;white-space:nowrap}
.subtab:hover{color:var(--text)}
.subtab.active{background:var(--surface);color:var(--accent);box-shadow:var(--shadow-sm)}

/* ===== 页内子导航（设置页顶部导航栏，划分功能子页） ===== */
.page-subtabs{margin:-14px 0 20px;border-radius:12px;padding:4px}
.page-subtab svg{width:15px;height:15px}

/* ===== 模型管理：工具栏 / 统计条 / 行列表 ===== */
.model-stats-bar{display:flex;flex-wrap:wrap;align-items:center;gap:6px 16px;padding:0 2px 12px;font-size:12px;color:var(--text2)}
.model-stats-bar b{font-variant-numeric:tabular-nums;color:var(--text);font-weight:600}
.model-stats-bar .dot{width:7px;height:7px;border-radius:50%;display:inline-block;margin-right:5px;vertical-align:1px}
.model-stats-bar .dot.g{background:var(--green)}
.model-stats-bar .dot.y{background:var(--yellow)}
.model-stats-bar .dot.r{background:var(--red)}
.model-toolbar{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin-bottom:12px}
.model-toolbar input[type="text"]{flex:2;min-width:180px;padding:8px 12px;font-size:13px}
.model-toolbar select{flex:1;min-width:120px;width:auto;padding:8px 10px;font-size:13px}
.model-list{border:1px solid var(--border2);border-radius:var(--radius-sm);overflow:hidden}
.model-list .model-group{margin:0}
.model-list .model-group + .model-group{border-top:1px solid var(--border2)}
.model-list .model-group-head{padding:9px 14px;margin:0}
.model-list .model-group-body{margin:0}
/* ===== 模型管理：两级分组（来源 → 厂商） ===== */
.model-group-src-head{font-weight:700;color:var(--text)}
.model-group-src > .model-group-body{border-left:2px solid var(--border2);padding-left:12px}
.model-list .model-group-body .model-group-body{margin:2px 0 4px 14px}
.model-group-vendor .model-group-head{font-weight:600}
.model-row{display:flex;align-items:center;gap:12px;padding:9px 14px;border-bottom:1px solid var(--border2);background:var(--surface);transition:background 0.15s var(--ease)}
.model-row:last-child{border-bottom:none}
.model-row:hover{background:var(--surface2)}
.model-row.disabled-row .model-row-id{color:var(--text3)}
.model-row-id{font-size:13px;font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.model-row-meta{font-size:11px;color:var(--text3);white-space:nowrap;margin-left:auto;font-variant-numeric:tabular-nums}
.model-row-actions{display:flex;align-items:center;gap:4px;flex-shrink:0}
.model-row-main{flex:1;min-width:0;display:flex;align-items:center;gap:6px}
.model-group-actions{margin-left:auto;display:flex;gap:4px;align-items:center}
.model-group-actions .btn{padding:3px 10px;font-size:11px}
.model-source-badge{display:inline-block;padding:1px 7px;border-radius:6px;font-size:10px;font-weight:500;background:var(--surface2);color:var(--text2);border:1px solid var(--border2);white-space:nowrap}
.model-source-badge.zen{background:var(--accent-soft);color:var(--accent);border-color:transparent}
.model-source-badge.custom{background:var(--green-soft);color:var(--green);border-color:transparent}
.model-source-badge.provider{background:var(--yellow-soft);color:var(--yellow);border-color:transparent}
.model-source-badge.remote{background:var(--surface2);color:var(--text2)}

/* ===== 模型组合（Combo） ===== */
.combo-check{display:inline-flex;align-items:center;gap:8px;font-size:14px;color:var(--text);cursor:pointer;padding:10px 0}
.combo-preview-box{margin-top:14px;border:1px solid var(--border2);border-radius:10px;padding:12px;background:var(--surface2)}
.combo-preview-title{font-size:12px;font-weight:600;color:var(--text2);margin-bottom:8px}
.combo-preview{font-size:13px;color:var(--text);line-height:1.9;word-break:break-word}
.combo-preview .step{display:inline-block;padding:2px 8px;border-radius:6px;background:var(--surface);border:1px solid var(--border2);font-family:ui-monospace,monospace;font-size:12px}
.combo-preview .arrow{color:var(--text3);margin:0 6px}
.combo-entry{display:flex;align-items:center;gap:8px;padding:8px 10px;border:1px solid var(--border2);border-radius:8px;background:var(--surface);margin-bottom:8px;flex-wrap:wrap}
.combo-entry.disabled-entry{opacity:0.6}
.combo-entry-idx{flex-shrink:0;width:22px;height:22px;border-radius:50%;background:var(--surface2);border:1px solid var(--border2);display:inline-flex;align-items:center;justify-content:center;font-size:11px;color:var(--text2);font-variant-numeric:tabular-nums}
.combo-entry-select{flex:1;min-width:200px}
.combo-entry-select select{padding:7px 10px;font-size:13px;font-family:ui-monospace,monospace}
.combo-entry-source{flex-shrink:0;display:inline-flex;align-items:center}
.combo-entry-source select{width:auto;min-width:104px;padding:7px 10px;font-size:12px}
.combo-entry-badges{display:flex;align-items:center;gap:4px;flex-wrap:wrap}
.combo-entry-actions{display:flex;align-items:center;gap:4px;margin-left:auto;flex-shrink:0}
.combo-entry-actions .btn{padding:3px 9px;font-size:12px}
.combo-empty-entries{font-size:13px;color:var(--text3);padding:8px 2px}
.combos-list{border:1px solid var(--border2);border-radius:var(--radius-sm);overflow:hidden}
.combos-list .row-count{font-size:12px;color:var(--text2);white-space:nowrap}

/* ===== Toggle 开关 ===== */
.switch{position:relative;display:inline-block;width:34px;height:20px;flex-shrink:0}
.switch input{opacity:0;width:0;height:0;position:absolute}
.switch .slider{position:absolute;inset:0;background:#c7c7cc;border-radius:20px;transition:all 0.18s var(--ease);cursor:pointer}
.switch .slider:before{content:"";position:absolute;width:16px;height:16px;left:2px;top:2px;background:#fff;border-radius:50%;transition:all 0.18s var(--ease);box-shadow:0 1px 3px rgba(0,0,0,0.25)}
.switch input:checked + .slider{background:var(--green)}
.switch input:checked + .slider:before{transform:translateX(14px)}
.switch input:disabled + .slider{opacity:0.5;cursor:wait}

/* ===== 交互 / 无障碍 / 一致性完善 ===== */
/* 键盘焦点环（表单控件已在 :focus 处理，避免双重描边） */
:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
input:focus-visible,textarea:focus-visible,select:focus-visible{outline:none}
.switch input:focus-visible + .slider{box-shadow:var(--ring)}
/* 工具栏控件统一高度，避免按钮 / 下拉 / 输入高低不齐 */
.model-toolbar{align-items:center}
.model-toolbar input:not([type="checkbox"]),.model-toolbar select,.model-toolbar .btn,.model-toolbar .sync-btn{min-height:var(--control-h)}
.model-toolbar .field{min-width:0}
/* 长文本稳健性：避免超长 URL / 模型 ID 撑破布局 */
.section-desc,.combo-preview,.log-attempts li,.log-req-model{overflow-wrap:break-word}
/* 弹层：统一遮罩模糊与阴影（登录 / 模型同步） */
#loginOverlay,#modelSyncOverlay{backdrop-filter:saturate(150%) blur(6px);-webkit-backdrop-filter:saturate(150%) blur(6px)}
#modelSyncModal{border-radius:var(--radius);box-shadow:var(--shadow-lg)}
/* 空 / 加载状态统一留白与行高 */
.empty{padding:var(--sp-8) var(--sp-5);color:var(--text2);line-height:1.6}
.empty-state{padding:var(--sp-8) var(--sp-6)}
/* 子标签 / 标签页横向滚动更顺滑 */
.subtabs,.tabs{-webkit-overflow-scrolling:touch;scrollbar-width:thin}
/* 尊重「减少动效」偏好 */
@media (prefers-reduced-motion:reduce){
  *,*::before,*::after{transition-duration:0.01ms !important;animation-duration:0.01ms !important;animation-iteration-count:1 !important}
}

@media (max-width:760px){
  .model-row{flex-wrap:wrap;gap:8px;padding:10px 12px}
  .model-row-meta{flex-basis:100%;margin-left:44px;order:3;white-space:normal}
  .model-toolbar input[type="text"]{flex-basis:100%}
  .model-toolbar select{flex:1 1 40%}
  .model-group-actions{width:100%;margin-left:0;justify-content:flex-end}
}

/* action row */
.action-row{display:flex;gap:8px;flex-wrap:wrap}

/* ===== Responsive ===== */
@media (max-width:760px){
  .layout{display:block}
  .sidebar{width:100%;height:auto;min-height:0;position:static;border-right:none;border-bottom:1px solid var(--border2)}
  .sidebar-header{padding:14px 16px;border-bottom:none}
  .sidebar-header .brand-sub,.nav-section-label,.sidebar-footer{display:none}
  .sidebar-lang{padding:0 12px 10px;border-top:none}
  .sidebar-lang .lang-switch{margin-top:10px}
  .sidebar-nav{flex:none;min-height:0;overflow:visible;padding-bottom:0}
  .nav-section{display:flex;padding:0 10px 12px;gap:4px;overflow-x:auto}
  .nav-item{flex:1;justify-content:center;gap:6px;margin:0;padding:8px 10px;min-width:max-content}
  .nav-item svg{width:18px;height:18px}
	  .main{padding:24px 16px 40px;max-width:none}
	  .content-shell{width:100%}
	  .large-title{font-size:28px;letter-spacing:0}
  .large-subtitle{margin-bottom:22px;font-size:14px}
  .page-header{align-items:flex-start;gap:14px;flex-direction:column;margin-bottom:22px}
	  .cards,.cards.tokens{grid-template-columns:repeat(2,minmax(0,1fr));gap:10px;margin-bottom:20px}
	  .card{padding:16px}
  .card .num{font-size:28px}
  .section{border-radius:14px;margin-bottom:16px}
  .section-title{padding:14px 16px 7px}
  .section-desc{padding:0 16px 10px}
  .section-body,.tab-content{padding:16px}
  .tabs{padding:0 12px;overflow-x:auto;white-space:nowrap}
  .tab{padding:11px 12px;font-size:13px}
  .form-row{flex-direction:column;align-items:stretch;gap:0;margin-bottom:0}
  .form-row .field{margin-bottom:14px}
  .form-actions,.action-row{gap:8px}
	  table{min-width:680px}
  .section-body.flush{overflow-x:auto}
  th,td{padding:11px 12px}
	  .toast{max-width:calc(100vw - 32px);width:max-content;text-align:center}
	}
	@media (min-width:761px) and (max-width:1180px){
	  .main{padding:30px 32px 56px}
	  .cards{grid-template-columns:repeat(2,minmax(0,1fr))}
	  .cards.tokens{grid-template-columns:repeat(3,minmax(0,1fr))}
	  .account-table th:nth-child(9),.account-table td:nth-child(9){display:none}
	}
	@media (max-width:760px){
	  .log-table{display:none}
	  .log-cards{display:grid;gap:10px;padding:12px}
	}
	@media (max-width:760px){
	  .account-table{display:none}
	  .account-cards{display:grid;gap:10px;padding:12px}
	  .account-card{border:1px solid var(--border2);border-radius:12px;padding:14px;background:var(--surface2)}
  .account-card-header{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:12px}
  .account-card .account-email{max-width:calc(100vw - 170px);min-width:0}
	  .account-metrics{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;margin-bottom:12px}
	  .account-metric{padding:8px 10px;border-radius:8px;background:var(--surface)}
	  .account-metric-label{display:block;color:var(--text2);font-size:11px;margin-bottom:2px}
	  .account-metric-value{font-size:14px;font-weight:600;font-variant-numeric:tabular-nums}
	  .account-card-footer{display:flex;align-items:center;justify-content:space-between;gap:12px;color:var(--text2);font-size:11px}
	  .account-card-actions{display:flex;gap:6px;flex-shrink:0}
	  .section-body.flush{overflow:visible}
	}
	@media (max-width:390px){
	  .cards,.cards.tokens{grid-template-columns:1fr}
	  .nav-item .nav-label{font-size:12px}
	  .action-row .btn{flex:1;justify-content:center}
	}
/* ===== 追加响应式：平板 / 工具栏换行 / 表格滚动 ===== */
@media (max-width:1000px){
  .model-toolbar .sync-btn{flex:1 1 auto;justify-content:center}
}
@media (max-width:900px){
  .page-header{flex-wrap:wrap}
  .model-toolbar input[type="text"]{flex-basis:100%}
  .model-toolbar select{flex:1 1 calc(50% - 8px)}
  .model-toolbar .btn,.model-toolbar .sync-btn{flex:1 1 auto;justify-content:center}
  .main{padding:28px 24px 48px}
}
@media (max-width:760px){
  .section-title{flex-wrap:wrap;gap:var(--sp-2)}
  .section-title > span{margin-left:0 !important}
  .combo-entry-select{flex:1 1 100%;min-width:0}
  .combo-entry-actions{margin-left:0}
  .log-cards{padding:var(--sp-3);gap:var(--sp-2)}
  .section-body.xscroll{padding:var(--sp-4)}
}
</style>
</head>
<body>
<div class="layout">
<div class="sidebar">
  <div class="sidebar-header">
    <div class="brand">
      <div class="logo"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg></div>
      <div>
        <div class="brand-name">Cline 代理</div>
        <div class="brand-sub">多账号轮询 · 双协议</div>
      </div>
    </div>
  </div>
  <div class="sidebar-nav">
  <div class="nav-section">
    <div class="nav-section-label">概览</div>
    <div class="nav-item active" data-tab="dashboard">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
      <span class="nav-label">仪表盘</span>
    </div>
  </div>
  <div class="nav-section">
    <div class="nav-section-label">账号</div>
    <div class="nav-item" data-tab="accounts">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
      <span class="nav-label">账号管理</span>
    </div>
    <div class="nav-item" data-tab="import">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
      <span class="nav-label">导入账号</span>
    </div>
    <div class="nav-item" data-tab="logs">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="8" y1="13" x2="16" y2="13"/><line x1="8" y1="17" x2="16" y2="17"/></svg>
      <span class="nav-label">请求日志</span>
    </div>
  </div>
  <div class="nav-section">
    <div class="nav-section-label">配置</div>
    <div class="nav-item" data-tab="routing">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="6" cy="6" r="3"/><circle cx="18" cy="18" r="3"/><path d="M6 9v3a3 3 0 0 0 3 3h6"/></svg>
      <span class="nav-label">路由与模型</span>
    </div>
    <div class="nav-item" data-tab="upstreams">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><line x1="6" y1="6" x2="6.01" y2="6"/><line x1="6" y1="18" x2="6.01" y2="18"/></svg>
      <span class="nav-label">上游服务</span>
    </div>
  </div>
  <div class="nav-section">
    <div class="nav-section-label">系统</div>
    <div class="nav-item" data-tab="security">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
      <span class="nav-label">密钥与安全</span>
    </div>
    <div class="nav-item" data-tab="about">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>
      <span class="nav-label">关于</span>
    </div>
  </div>
  </div>
  <div class="sidebar-footer">
    <div style="display:flex;align-items:center;gap:6px;margin-bottom:6px">
      <span style="font-weight:600;color:var(--text)">Cline2API</span>
      <span id="footerVersion" style="font-size:11px;opacity:0.7">dev</span>
    </div>
    <div style="margin-bottom:4px">API: <span id="footerApiAddr">127.0.0.1:3457</span></div>
    <div><a href="#" onclick="openExternal('https://github.com/luawei1/cline2api');return false">GitHub</a> · <a href="#" onclick="openExternal('https://github.com/luawei1/cline2api/issues');return false">反馈</a> · MIT</div>
  </div>
  <div class="sidebar-lang">
    <div class="lang-switch">
      <button type="button" id="langZh" onclick="setLang('zh')">中文</button>
      <button type="button" id="langEn" onclick="setLang('en')">English</button>
    </div>
  </div>
</div>

<div class="main">
<div class="content-shell">

<div id="tab-dashboard" class="tab-panel">
  <div class="large-title">仪表盘</div>
  <div class="large-subtitle">查看账号池状态与快捷操作</div>
  <div class="metric-section">
    <div class="metric-heading">账号状态</div>
    <div class="cards">
      <div class="card blue">
      <div class="card-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg></div>
      <div class="num" id="statTotal">-</div><div class="label">账号总数</div>
    </div>
    <div class="card green">
      <div class="card-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg></div>
      <div class="num" id="statActive">-</div><div class="label">活跃</div>
    </div>
    <div class="card yellow">
      <div class="card-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg></div>
      <div class="num" id="statCooldown">-</div><div class="label">冷却</div>
    </div>
    <div class="card red">
      <div class="card-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg></div>
      <div class="num" id="statExpired">-</div><div class="label">已过期</div>
    </div>
  </div>
  </div>
  <div class="metric-section">
    <div class="metric-heading">Token 用量</div>
    <div class="cards tokens">
    <div class="card blue">
      <div class="card-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"/></svg></div>
      <div class="num" id="statPromptTokens">-</div><div class="label">累计输入 Token</div>
    </div>
    <div class="card green">
      <div class="card-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"/></svg></div>
      <div class="num" id="statCompletionTokens">-</div><div class="label">累计输出 Token</div>
    </div>
    <div class="card yellow">
      <div class="card-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"/></svg></div>
      <div class="num" id="statTotalTokens">-</div><div class="label">累计总 Token</div>
    </div>
    <div class="card blue">
      <div class="card-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2a10 10 0 1 0 10 10"/><path d="M12 6v6l4 2"/></svg></div>
      <div class="num" id="statCachedTokens">-</div><div class="label">缓存 Token</div>
    </div>
    </div>
  </div>
  <div class="metric-section">
    <div class="metric-heading">opencode 免费模型 · 今日用量</div>
    <div class="cards tokens">
      <div class="card yellow">
      <div class="card-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg></div>
      <div class="num" id="statOcRequests">-</div><div class="label">今日请求数</div>
    </div>
    <div class="card blue">
      <div class="card-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"/></svg></div>
      <div class="num" id="statOcInputTokens">-</div><div class="label">今日输入 Token</div>
    </div>
    <div class="card green">
      <div class="card-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"/></svg></div>
      <div class="num" id="statOcOutputTokens">-</div><div class="label">今日输出 Token</div>
    </div>
    <div class="card yellow">
      <div class="card-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg></div>
      <div class="num" id="statOcTotalTokens">-</div><div class="label">今日总 Token</div>
    </div>
    </div>
  </div>
  <div class="metric-section">
    <div class="metric-heading" style="display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:8px">
      <span>模型用量统计</span>
      <span style="display:flex;gap:6px">
        <button class="btn btn-sm" id="modelUsageBtnToday" onclick="switchModelUsageRange('today')">今日</button>
        <button class="btn btn-sm" id="modelUsageBtnAll" onclick="switchModelUsageRange('all')">累计</button>
      </span>
    </div>
    <div class="section-body" style="margin-top:10px">
      <table>
        <thead><tr>
          <th style="width:32%">模型</th>
          <th style="width:14%">通道</th>
          <th style="width:10%;text-align:right">请求数</th>
          <th style="width:11%;text-align:right">输入</th>
          <th style="width:11%;text-align:right">输出</th>
          <th style="width:11%;text-align:right">缓存</th>
          <th style="width:11%;text-align:right">总计</th>
        </tr></thead>
        <tbody id="modelUsageBody">
          <tr><td colspan="7" class="empty">加载中...</td></tr>
        </tbody>
      </table>
    </div>
  </div>
  <div class="section">
    <div class="section-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>快捷操作</div>
    <div class="section-body action-row">
      <button class="btn btn-primary" onclick="switchTab('import')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>添加账号</button>
      <button class="btn" onclick="refreshAllTokens()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><polyline points="1 20 1 14 7 14"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg>刷新全部 Token</button>
      <button class="btn" onclick="document.getElementById('fileInput').click()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>从文件导入</button>
      <input type="file" id="fileInput" accept=".json,.txt" style="display:none" onchange="handleFileImport(event)">
      <button class="btn" onclick="switchTab('security');switchPageSubTab('security','keys');generateKey()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg>生成 API 密钥</button>
    </div>
  </div>
</div>

<div id="tab-accounts" class="tab-panel" style="display:none">
  <div class="page-header">
    <div>
      <div class="large-title">账号管理</div>
      <div class="large-subtitle">管理 Cline 账号池中的所有账号</div>
    </div>
    <div style="display:flex;gap:8px">
      <button class="btn btn-sm" onclick="testAllAccounts(this)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>测试全部</button>
      <button class="btn btn-sm" onclick="exportSelectedAccounts()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>导出选中<span id="selCount"></span></button>
      <button class="btn btn-sm" onclick="resetSelectedAccounts(this)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><polyline points="1 20 1 14 7 14"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg>重置选中<span id="resetCount"></span></button>
      <button class="btn btn-sm btn-danger" onclick="deleteSelectedAccounts(this)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>删除选中<span id="delCount"></span></button>
      <button class="btn btn-sm" onclick="exportAccounts()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>导出全部</button>
      <button class="btn btn-primary btn-sm" onclick="switchTab('import')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>添加</button>
      <button class="btn btn-sm" onclick="loadAccounts()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><polyline points="1 20 1 14 7 14"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg>刷新</button>
    </div>
  </div>
  <div class="section">
    <div class="section-body flush">
      <div style="display:flex;gap:8px;align-items:center;padding:10px 16px;border-bottom:1px solid var(--border2)">
        <input type="text" id="accSearch" placeholder="搜索邮箱..." oninput="onAccSearch(this.value)" style="flex:1;max-width:320px;padding:7px 12px;border-radius:8px;border:1px solid var(--border2);background:var(--surface2);color:var(--text);font-size:13px">
        <span id="accSearchCount" style="font-size:12px;color:var(--text3)"></span>
      </div>
      <table class="account-table">
        <thead>
          <tr><th style="width:30px;text-align:center"><input type="checkbox" id="accSelAll" onchange="toggleAllAccountSel(this)" title="全选本页"></th><th style="width:40px">#</th><th onclick="onAccSort('email')" style="cursor:pointer" title="点击排序">邮箱</th><th onclick="onAccSort('status')" style="cursor:pointer" title="点击排序">状态</th><th onclick="onAccSort('usage')" style="cursor:pointer" title="点击排序">请求</th><th>输入</th><th>输出</th><th onclick="onAccSort('total')" style="cursor:pointer" title="点击排序">总 Token</th><th onclick="onAccSort('cached')" style="cursor:pointer" title="点击排序">缓存</th><th onclick="onAccSort('lastUsed')" style="cursor:pointer" title="点击排序">最后使用</th><th onclick="onAccSort('createdAt')" style="cursor:pointer" title="点击排序">创建时间</th><th>操作</th></tr>
        </thead>
        <tbody id="accountTableBody">
          <tr><td colspan="12" class="empty">加载中...</td></tr>
        </tbody>
      </table>
      <div id="accountPager" style="display:none;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:8px;padding:10px 16px;border-top:1px solid var(--border2)"></div>
      <div id="accountCards" class="account-cards"></div>
    </div>
  </div>
</div>

<div id="tab-import" class="tab-panel" style="display:none">
  <div class="large-title">导入账号</div>
  <div class="large-subtitle">通过 OAuth 登录、手动 Token 或批量文件添加账号</div>
  <div class="section">
    <div class="tabs" id="importTabs">
      <div class="tab active" data-tab="oauth">OAuth 浏览器登录</div>
      <div class="tab" data-tab="token">手动输入 Token</div>
      <div class="tab" data-tab="batch">批量导入</div>
    </div>

    <div id="import-oauth" class="tab-content active">
      <p style="color:var(--text2);margin-bottom:16px">通过浏览器完成 OAuth 认证，支持 Google/GitHub/邮箱登录，自动获取 refreshToken。</p>
      <button class="btn btn-primary" onclick="startOAuth()" id="oauthBtn"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M12 5l7 7-7 7"/></svg>开始 OAuth 登录</button>
      <div id="oauthProgress" style="display:none;margin-top:16px">
        <div style="display:flex;align-items:center;gap:12px">
          <div class="loading"></div>
          <div>
            <div style="font-weight:600" id="oauthStatus">等待浏览器授权...</div>
            <div style="color:var(--text2);font-size:13px;margin-top:4px">
              点击链接（系统浏览器打开）: <a href="#" id="oauthUrl" style="color:var(--accent);cursor:pointer"></a><br>
              并输入代码: <strong id="oauthUserCode"></strong>
            </div>
          </div>
        </div>
      </div>
      <div id="oauthResult" style="display:none;margin-top:16px"></div>
    </div>

    <div id="import-token" class="tab-content">
      <p style="color:var(--text2);margin-bottom:16px">输入已有的 Cline refreshToken，系统会自动验证并加入池。</p>
      <div class="form-row">
        <div class="field">
          <label>Refresh Token *</label>
          <input type="text" id="tokenInput" placeholder="粘贴 refreshToken">
        </div>
      </div>
      <div class="form-row">
        <div class="field">
          <label>邮箱（可选，留空自动生成）</label>
          <input type="text" id="tokenEmail" placeholder="user@example.com">
        </div>
      </div>
      <div class="form-actions">
        <button class="btn btn-primary" onclick="addByToken()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>添加账号</button>
      </div>
      <div id="tokenResult" style="margin-top:8px"></div>
    </div>

    <div id="import-batch" class="tab-content">
      <p style="color:var(--text2);margin-bottom:16px">批量导入多个账号。支持 JSON 数组或每行一个 token。</p>
      <div class="form-row">
        <div class="field">
          <label>JSON 数组格式：[{"refreshToken":"...","email":"..."}]</label>
          <textarea id="batchInput" placeholder='[{"refreshToken":"xxx","email":"u1@x.com"},{"refreshToken":"yyy","email":"u2@x.com"}]'></textarea>
        </div>
      </div>
      <div class="form-actions">
        <button class="btn btn-primary" onclick="batchImport()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>导入全部</button>
        <button class="btn" onclick="document.getElementById('fileInput2').click()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>选择文件</button>
        <input type="file" id="fileInput2" accept=".json,.txt" style="display:none" onchange="handleFileImport(event)">
      </div>
      <div id="batchResult" style="margin-top:8px"></div>
    </div>
  </div>
</div>

<div id="tab-logs" class="tab-panel" style="display:none">
  <div class="page-header">
    <div>
      <div class="large-title">请求日志</div>
      <div class="large-subtitle">查看每次请求的 Token 消耗、缓存、耗时与流式速度</div>
    </div>
    <div style="display:flex;gap:8px">
      <button class="btn btn-sm" onclick="loadRequestLogs(true)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><polyline points="1 20 1 14 7 14"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg>刷新</button>
    </div>
  </div>
  <div class="section">
    <div class="section-body flush">
      <table class="log-table">
        <thead>
          <tr><th>时间</th><th>账号</th><th>协议</th><th>模型</th><th>输入</th><th>输出</th><th>缓存</th><th>总</th><th>耗时</th><th>TTFT</th><th>tok/s</th><th>状态</th></tr>
        </thead>
        <tbody id="logTableBody">
          <tr><td colspan="12" class="empty">加载中...</td></tr>
        </tbody>
      </table>
      <div id="logCards" class="log-cards"></div>
    </div>
  </div>
  <div id="logLoadMore" style="display:none;text-align:center;padding:16px">
    <button class="btn btn-primary" onclick="loadRequestLogs(false)">加载更多</button>
  </div>
</div>

<div id="tab-security" class="tab-panel" style="display:none">
  <div class="page-header">
    <div>
      <div class="large-title">密钥与安全</div>
      <div class="large-subtitle">API 密钥、访问控制与危险操作</div>
    </div>
  </div>

  <div class="subtabs page-subtabs" data-page="security">
    <div class="subtab active" data-sub="keys">API 密钥</div>
    <div class="subtab" data-sub="access">访问设置</div>
    <div class="subtab" data-sub="danger">危险操作</div>
  </div>

  <div class="page-subpage" data-subpage="keys">
  <div class="section">
    <div class="section-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg>API 密钥管理</div>
    <div class="section-desc">生成的密钥可用于客户端访问代理 API（作为 x-api-key 或 Authorization 头）。</div>
    <div class="section-body">
      <div class="form-actions" style="margin-bottom:14px">
        <button class="btn btn-success" onclick="generateKey()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>生成新密钥</button>
      </div>
      <div id="keysList"></div>
      <div id="keyGenResult" style="margin-top:8px"></div>
    </div>
  </div>
  </div>

  <div class="page-subpage" data-subpage="access" style="display:none">
  <div class="section">
    <div class="section-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>访问设置</div>
    <div class="section-body">
      <div class="form-row">
        <div class="field">
          <label>监听地址</label>
          <select id="settingListenHost" onchange="saveListenHost()">
            <option value="127.0.0.1">127.0.0.1（仅本机）</option>
            <option value="0.0.0.0">0.0.0.0（所有网卡）</option>
          </select>
        </div>
        <div class="field"><label>当前地址</label><input type="text" id="settingAddr" disabled></div>
      </div>
      <div id="listenWarn" class="warn-box" style="display:none">⚠️ 当前监听非本机回环地址（0.0.0.0 或局域网 IP），管理后台无鉴权，局域网内任何设备都可访问。请确认网络环境安全，或配合防火墙限制端口。</div>
      <div id="localIPsRow" class="form-row" style="display:none">
        <div class="field" style="flex:1">
          <label>本机 IP（局域网访问地址）</label>
          <div id="localIPsList"></div>
        </div>
      </div>
      <div class="form-row" style="margin-top:12px">
        <div class="field" style="flex:1">
          <label>管理后台密码（<span id="passwordStatus">未启用</span>）</label>
          <div style="display:flex;gap:8px">
            <input type="password" id="settingPassword" placeholder="留空保存 = 清除密码" autocomplete="new-password" style="flex:1">
            <button class="btn btn-primary" onclick="savePassword()">保存</button>
          </div>
          <div style="font-size:12px;color:var(--text3);margin-top:4px">设置后访问管理后台需输入密码，默认无密码</div>
        </div>
      </div>
    </div>
  </div>
  </div>

  <div class="page-subpage" data-subpage="danger" style="display:none">
  <div class="section">
    <div class="section-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>危险操作</div>
    <div class="section-body">
      <div class="action-row">
        <button class="btn btn-danger" onclick="deleteAllAccounts()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>删除全部账号</button>
        <button class="btn btn-danger" onclick="deleteAllKeys()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>删除全部密钥</button>
      </div>
    </div>
  </div>
  </div>
</div>

<div id="tab-routing" class="tab-panel" style="display:none">
  <div class="page-header">
    <div>
      <div class="large-title">路由与模型</div>
      <div class="large-subtitle">默认模型、轮询策略、回退链与可用模型</div>
    </div>
  </div>

  <div class="subtabs page-subtabs" data-page="routing">
    <div class="subtab active" data-sub="config">路由配置</div>
    <div class="subtab" data-sub="models">模型管理</div>
    <div class="subtab" data-sub="combos">模型组合</div>
  </div>

  <div class="page-subpage" data-subpage="config">
  <div class="section">
    <div class="section-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>代理配置</div>
    <div class="section-body">
      <div class="form-row">
        <div class="field"><label>默认模型</label><select id="settingDefModel" onchange="updateConfig()"><option value="">加载中...</option></select></div>
      </div>
      <div class="form-row">
        <div class="field">
          <label>轮询策略</label>
          <select id="settingStrategy" onchange="updateConfig()">
            <option value="round_robin">轮询 (round_robin)</option>
            <option value="fill">填满 (fill)</option>
            <option value="random">随机 (random)</option>
            <option value="quality">质量优先 (quality)</option>
          </select>
        </div>
        <div class="field"><label>引擎版本</label><input type="text" id="settingVersion" disabled></div>
      </div>
      <div class="form-row">
        <div class="field">
          <label>只显示免费模型</label>
          <select id="settingOnlyFree" onchange="updateConfig()">
            <option value="false">关闭（返回全部模型）</option>
            <option value="true">开启（/models 仅返回免费模型）</option>
          </select>
          <div style="font-size:12px;color:var(--text3);margin-top:4px">影响 /models、/v1/models 对客户端返回的模型列表，不影响管理页模型管理</div>
        </div>
        <div class="field">
          <label>回退模型链</label>
          <input type="text" id="settingModelChain" placeholder="z-ai/glm-5.3-flash, deepseek/deepseek-v4-flash, cline-free/longcat-2.0, free" oninput="this.dataset.dirty='1'" onchange="updateConfig()">
          <div style="font-size:12px;color:var(--text3);margin-top:4px">逗号分隔模型 ID，可用 <code>free</code> 表示内置免费回退链；留空则自动回退到内置免费链。允许填写不在上方列表里的模型 ID（列表外的模型实测仍可用时可直接填入，同步列表只是参考）；失效条目由冷却与故障转移自动跳过</div>
        </div>
      </div>
      <div class="form-row">
        <div class="field"><label>账号文件</label><input type="text" id="settingPoolPath" disabled></div>
      </div>
    </div>
  </div>
  </div>

  <div class="page-subpage" data-subpage="models" style="display:none">
  <div class="section">
    <div class="section-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><line x1="9" y1="9" x2="15" y2="9"/><line x1="9" y1="15" x2="15" y2="15"/></svg><span>模型管理</span>
      <span style="margin-left:auto;display:flex;align-items:center;gap:10px;font-size:12px;font-weight:400;color:var(--text3)">
        <span><span>上次同步</span>: <span id="modelSyncTime">从未同步</span></span>
      </span>
    </div>
    <div class="section-desc">管理从 Cline / opencode 同步的模型与自定义模型。停用的模型不会出现在客户端模型列表（/v1/models）中，直接请求也会被拒绝。</div>
    <div class="section-body">
      <div id="modelStatsBar" class="model-stats-bar"></div>
      <div class="model-toolbar">
        <input type="text" id="modelSearch" placeholder="搜索模型 ID..." oninput="onModelFilterChange('search', this.value)">
        <select id="modelFilterProvider" onchange="onModelFilterChange('provider', this.value)"><option value="">全部来源</option></select>
        <select id="modelFilterCost" onchange="onModelFilterChange('cost', this.value)">
          <option value="">全部计费</option>
          <option value="free">免费</option>
          <option value="pass">付费</option>
        </select>
        <select id="modelFilterStatus" onchange="onModelFilterChange('status', this.value)">
          <option value="">全部状态</option>
          <option value="enabled">已启用</option>
          <option value="disabled">已停用</option>
        </select>
        <button class="btn btn-sm" onclick="toggleAddModelRow()">＋ 添加模型</button>
        <button class="btn btn-sm" id="bulkEnableFreeBtn" onclick="bulkModelToggle('free')">启用全部免费</button>
        <button class="btn btn-sm" id="bulkDisablePaidBtn" onclick="bulkModelToggle('paid')">停用全部付费</button>
        <button class="sync-btn" id="syncModelsBtn" onclick="syncModels()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width:14px;height:14px"><polyline points="23 4 23 10 17 10"/><polyline points="1 20 1 14 7 14"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg><span>从 Cline 同步模型</span></button>
        <button class="sync-btn" id="syncOcModelsBtn" onclick="syncOcModels()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width:14px;height:14px"><polyline points="23 4 23 10 17 10"/><polyline points="1 20 1 14 7 14"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg><span>从 opencode 同步模型</span></button>
      </div>
      <div id="modelAddRow" class="model-toolbar" style="display:none">
        <div class="field" style="flex:2;min-width:220px">
          <label>添加模型</label>
          <input type="text" id="newModelId" placeholder="如 deepseek/deepseek-v4-flash" style="font-family:ui-monospace,monospace">
        </div>
        <div class="field" style="flex:1;min-width:120px">
          <label>计费</label>
          <select id="newModelCost">
            <option value="pass">付费 (pass)</option>
            <option value="free">免费 (free)</option>
          </select>
        </div>
        <button class="btn btn-success" onclick="addModel()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>添加</button>
      </div>
      <div id="modelsList" class="model-list">加载中...</div>
    </div>
  </div>
  </div>

  <div class="page-subpage" data-subpage="combos" style="display:none">
  <div class="section">
    <div class="section-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg><span>模型组合</span>
      <span style="margin-left:auto;display:flex;align-items:center;gap:8px;font-size:12px;font-weight:400;color:var(--text3)">
        <button class="btn btn-sm btn-primary" onclick="showComboEditor()">＋ 新建组合</button>
      </span>
    </div>
    <div class="section-desc">把多个模型按优先级编排成组合（Combo）：命中组合时按顺序依次尝试，一个模型失败（限流/不可用）就自动切到下一个。停用的条目会被直接跳过。</div>
    <div class="section-body">
      <div id="combosList">加载中...</div>

      <div id="comboEditor" style="display:none;margin-top:14px;border:1px solid var(--border2);border-radius:10px;padding:14px">
        <div class="form-row">
          <div class="field"><label>组合 ID *</label><input type="text" id="comboId" placeholder="premium-coding" style="font-family:ui-monospace,monospace"></div>
          <div class="field"><label>名称 *</label><input type="text" id="comboName" placeholder="Premium Coding"></div>
        </div>
        <div class="form-row">
          <div class="field" style="flex:2"><label>描述</label><input type="text" id="comboDesc" placeholder="可选"></div>
          <div class="field"><label>分类</label><input type="text" id="comboCategory" placeholder="coding（可选）"></div>
        </div>
        <div class="form-row">
          <div class="field">
            <label>启用</label>
            <select id="comboEnabled"><option value="true">启用</option><option value="false">停用</option></select>
          </div>
          <div class="field" style="justify-content:flex-end">
            <label>紧急回退</label>
            <label class="combo-check"><input type="checkbox" id="comboEmergency" style="width:16px;height:16px;accent-color:var(--accent);cursor:pointer"> <span>允许跳出组合回退</span></label>
          </div>
        </div>
        <div style="font-size:12px;color:var(--text3);margin:-6px 0 12px">开启「紧急回退」后，仅当组合内所有条目都失败时，才允许继续回退到配置的全局回退链 / 内置免费链；关闭则组合内失败即结束。</div>

        <div style="font-size:13px;color:var(--text2);font-weight:500;margin-bottom:8px">模型条目（按优先级从上到下）</div>
        <div id="comboEntries"></div>
        <div class="form-actions" style="margin-top:10px">
          <button class="btn btn-sm" onclick="addComboEntry()">＋ 添加条目</button>
        </div>

        <div class="combo-preview-box">
          <div class="combo-preview-title">优先级预览</div>
          <div id="comboPreview" class="combo-preview">（暂无启用的条目）</div>
          <div style="font-size:11px;color:var(--text3);margin-top:6px">仅显示已启用的条目，停用的条目会被跳过。</div>
        </div>

        <div class="form-actions" style="margin-top:14px">
          <button class="btn btn-sm btn-success" onclick="saveCombo()">保存组合</button>
          <button class="btn btn-sm" onclick="hideComboEditor()">取消</button>
          <span id="comboSaveResult" style="font-size:12px;margin-left:8px"></span>
        </div>
      </div>
    </div>
  </div>
  </div>
</div>

<div id="tab-upstreams" class="tab-panel" style="display:none">
  <div class="page-header">
    <div>
      <div class="large-title">上游服务</div>
      <div class="large-subtitle">opencode zen、Cline 出口代理、请求头与自定义 Provider</div>
    </div>
  </div>
  <div class="subtabs page-subtabs" data-page="upstreams">
    <div class="subtab active" data-sub="zen">opencode Zen</div>
    <div class="subtab" data-sub="cline">Cline 出口代理</div>
    <div class="subtab" data-sub="headers">请求头</div>
    <div class="subtab" data-sub="providers">自定义 Provider</div>
  </div>

  <div class="page-subpage" data-subpage="zen">
  <div class="section">
    <div class="section-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg><span>opencode 免费模型</span>
      <span style="margin-left:auto;display:flex;align-items:center;gap:10px;font-size:12px;font-weight:400;color:var(--text3)">
        <span id="ocRuntimeStatus">-</span>
      </span>
    </div>
    <div class="section-desc">接入 opencode（zen）免费模型。按请求中的模型名自动分流：免费模型走 opencode 上游，付费模型直接拒绝，其余走 Cline 账号池。</div>
    <div class="section-body">
      <div class="form-row">
        <div class="field" style="max-width:220px">
          <label>启用 opencode</label>
          <select id="ocEnabled" onchange="saveOcConfig()">
            <option value="true">启用</option>
            <option value="false">停用</option>
          </select>
        </div>
        <div class="field" style="flex:1"><label>API Key</label><input type="text" id="ocKey" placeholder="public" onchange="saveOcConfig()"></div>
        <div class="field" style="flex:1.4"><label>Base URL</label><input type="text" id="ocBaseURL" placeholder="https://opencode.ai/zen/v1" style="font-family:ui-monospace,monospace" onchange="saveOcConfig()"></div>
      </div>
      <div class="form-row" style="margin-top:12px">
        <div class="field"><label>最大并发</label><input type="number" id="ocMaxConcurrency" min="1" max="64" onchange="saveOcConfig()"></div>
        <div class="field"><label>重试次数</label><input type="number" id="ocRetries" min="0" max="10" onchange="saveOcConfig()"></div>
        <div class="field"><label>故障转移</label>
          <select id="ocFailover" onchange="saveOcConfig()">
            <option value="true">开启（连续失败后暂走 Cline 池）</option>
            <option value="false">关闭</option>
          </select>
        </div>
        <div class="field"><label>失败阈值（次）</label><input type="number" id="ocFailoverCount" min="1" max="20" onchange="saveOcConfig()"></div>
        <div class="field"><label>转移窗口（分钟）</label><input type="number" id="ocFailoverMinutes" min="1" max="120" onchange="saveOcConfig()"></div>
      </div>
      <div class="form-row" style="margin-top:12px">
        <div class="field"><label>自动上下文压缩</label>
          <select id="ocCompactAuto" onchange="saveOcConfig()">
            <option value="true">开启（超限自动摘要）</option>
            <option value="false">关闭</option>
          </select>
        </div>
        <div class="field"><label>压缩缓冲 Token</label><input type="number" id="ocCompactBuffer" min="0" onchange="saveOcConfig()"></div>
        <div class="field"><label>尾部保留 Token</label><input type="number" id="ocCompactKeepTokens" min="0" onchange="saveOcConfig()"></div>
        <div class="field"><label>摘要最大 Token</label><input type="number" id="ocCompactMaxSummary" min="0" onchange="saveOcConfig()"></div>
      </div>
      <div class="form-actions" style="margin-top:14px">
        <button class="btn btn-primary" onclick="saveOcConfig()">手动保存 opencode 配置</button>
      </div>
      <div id="ocSaveResult" style="margin-top:8px"></div>
      <div style="font-size:12px;color:var(--text3);margin-top:8px">上述字段改动后会自动保存，也可点按钮手动触发。</div>
    </div>
  </div>

  <div class="section">
    <div class="section-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>opencode 出口代理</div>
    <div class="section-desc">发往 opencode 的请求可经代理池轮询出口；命中限流时冷却当前出口并自动跳过。支持 http / https / socks5 / socks5h，每行一个，如 <span class="mono">socks5://127.0.0.1:1080</span>。</div>
    <div class="section-body">
      <div class="form-row">
        <div class="field" style="max-width:260px">
          <label>代理策略</label>
          <select id="ocProxyStrategy" onchange="saveOcConfig()">
            <option value="round_robin">轮询 (round_robin)</option>
            <option value="random">随机 (random)</option>
            <option value="fill">填满 (fill)</option>
          </select>
        </div>
        <div class="field" style="flex:1"><label>出口冷却状态</label><div id="ocProxyCooldowns" style="font-size:12px;color:var(--text3)">-</div></div>
      </div>
      <div class="field" style="margin-top:10px">
        <label>代理列表</label>
        <textarea id="ocProxies" rows="4" style="width:100%;font-family:ui-monospace,monospace;font-size:12px;border:1px solid var(--border2);border-radius:8px;padding:8px;background:var(--surface);color:var(--text)" placeholder="socks5://127.0.0.1:1080&#10;http://user:pass@proxy.example.com:8080" onchange="saveOcConfig()"></textarea>
      </div>
    </div>
  </div>

  </div>

  <div class="page-subpage" data-subpage="cline" style="display:none">
  <div class="section">
    <div class="section-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>Cline 出口代理</div>
    <div class="section-desc">国内直连 Cline 上游会被跨区限制；配置后所有发往 Cline 的请求（对话、登录/令牌刷新、模型同步）经代理池轮询出去。支持 http / https / socks5 / socks5h，每行一个，如 <span class="mono">socks5://127.0.0.1:1080</span>。回环 / 内网地址（本机 Ollama 等自定义 Provider）始终直连；未配置时依次尝试环境变量代理、直连。</div>
    <div class="section-body">
      <div class="form-row">
        <div class="field" style="max-width:260px">
          <label>代理策略</label>
          <select id="clineProxyStrategy">
            <option value="round_robin">轮询 (round_robin)</option>
            <option value="random">随机 (random)</option>
            <option value="fill">填满 (fill)</option>
          </select>
        </div>
      </div>
      <div class="field" style="margin-top:10px">
        <label>代理列表</label>
        <textarea id="clineProxies" rows="4" style="width:100%;font-family:ui-monospace,monospace;font-size:12px;border:1px solid var(--border2);border-radius:8px;padding:8px;background:var(--surface);color:var(--text)" placeholder="socks5://127.0.0.1:1080&#10;http://user:pass@proxy.example.com:8080"></textarea>
      </div>
      <div class="form-actions" style="margin-top:14px">
        <button class="btn btn-primary" onclick="saveClineProxyConfig()">保存 Cline 代理</button>
      </div>
      <div id="clineProxySaveResult" style="margin-top:8px"></div>
    </div>
  </div>
  </div>

  <div class="page-subpage" data-subpage="headers" style="display:none">
  <div class="section">
    <div class="section-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7V4h16v3"/><path d="M9 20h6"/><path d="M12 4v16"/></svg>Cline 请求头</div>
    <div class="section-desc">这些请求头会附加到所有转发给 Cline API 的请求中，以模拟官方客户端行为。</div>
    <div class="section-body xscroll">
      <table>
        <thead><tr><th style="width:240px">请求头</th><th>值</th><th style="width:48px"></th></tr></thead>
        <tbody id="headersTableBody">
          <tr><td colspan="3" class="empty">加载中...</td></tr>
        </tbody>
      </table>
      <div class="form-actions" style="margin-top:14px">
        <button class="btn btn-sm" onclick="addHeaderRow()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>添加请求头</button>
        <button class="btn btn-sm btn-primary" onclick="saveHeaders()"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/></svg>保存请求头</button>
        <button class="btn btn-sm" onclick="bulkEditHeaders()">批量编辑</button>
      </div>
      <div id="headerSaveResult" style="margin-top:8px"></div>
    </div>
  </div>

  <div class="section">
    <div class="section-title">opencode 请求头</div>
    <div class="section-desc">附加到发往 opencode zen 的请求。特殊值：$session / $request / $project / $client 会注入每请求的动态身份；留空删除该头。</div>
    <div class="section-body">
      <div class="field">
        <label>请求头（每行一个，格式 Key: Value）</label>
        <textarea id="zenHeadersBulk" rows="6" style="width:100%;font-family:ui-monospace,monospace;font-size:12px;border:1px solid var(--border2);border-radius:8px;padding:8px;background:var(--surface);color:var(--text)" placeholder="User-Agent: opencode/1.18.31&#10;x-opencode-project: global&#10;x-custom-header: my-value"></textarea>
      </div>
      <div class="form-actions" style="margin-top:10px">
        <button class="btn btn-sm btn-primary" onclick="saveZenHeaders()">保存 opencode 请求头</button>
      </div>
      <div id="zenHeaderSaveResult" style="margin-top:8px"></div>
    </div>
  </div>

  </div>

  <div class="page-subpage" data-subpage="providers" style="display:none">
  <div class="section">
    <div class="section-title">自定义 Provider（OpenAI 兼容）</div>
    <div class="section-desc">接入任意 OpenAI 兼容上游（OpenRouter / Groq / Cerebras / Gemini / Mistral / Together / 自建 vLLM 等）。模型命中自定义 Provider 时优先走该上游，失败自动按回退链降级，最终兜底 Cline 池。</div>
    <div class="section-body xscroll">
      <div id="providersList"></div>
      <div class="form-actions" style="margin-top:10px">
        <button class="btn btn-sm btn-primary" onclick="showProviderEditor()">＋ 添加 Provider</button>
        <button class="btn btn-sm" onclick="loadProviderPresets()">从预设添加（免费源）</button>
      </div>
      <div id="providerEditor" style="display:none;margin-top:14px;border:1px solid var(--border2);border-radius:10px;padding:14px">
        <div class="form-row">
          <div class="field"><label>名称 *</label><input type="text" id="provName" placeholder="OpenRouter"></div>
          <div class="field" style="flex:1.6"><label>Base URL *</label><input type="text" id="provBaseURL" placeholder="https://openrouter.ai/api/v1" style="font-family:ui-monospace,monospace"></div>
        </div>
        <div class="form-row">
          <div class="field" style="flex:1.4"><label>API Key</label><input type="password" id="provKey" placeholder="sk-...（本地服务器可留空）"></div>
          <div class="field"><label>优先级（小=优先）</label><input type="number" id="provPriority" value="10" min="0" max="1000"></div>
          <div class="field"><label>超时(秒)</label><input type="number" id="provTimeout" value="300" min="5" max="600"></div>
        </div>
        <div class="form-row">
          <div class="field"><label>分钟请求上限（0=不限）</label><input type="number" id="provRpmLimit" value="0" min="0" max="100000"></div>
          <div class="field"><label>每日请求上限（0=不限）</label><input type="number" id="provDailyLimit" value="0" min="0" max="1000000"></div>
        </div>
        <div style="font-size:12px;color:var(--text3);margin-bottom:8px">配额提示：填入免费层限额后，路由会在派发前自动避开打满额度的上游，改用组内其他 provider，不再拿 429 试错</div>
        <div class="form-row">
          <div class="field" style="flex:1"><label>模型 ID（逗号分隔，如 z-ai/glm-5.3-flash, deepseek/deepseek-v4-flash）</label><input type="text" id="provModels" style="font-family:ui-monospace,monospace"></div>
        </div>
        <div class="form-row">
          <div class="field" style="flex:1"><label>自定义请求头（每行 Key: Value，如 HTTP-Referer: https://mysite.com）</label><textarea id="provHeaders" rows="3" style="width:100%;font-family:ui-monospace,monospace;font-size:12px;border:1px solid var(--border2);border-radius:8px;padding:8px;background:var(--surface);color:var(--text)"></textarea></div>
        </div>
        <div class="form-row">
          <div class="field">
            <label>启用</label>
            <select id="provEnabled"><option value="true">启用</option><option value="false">停用</option></select>
          </div>
          <div class="field">
            <label>免费来源（供统计）</label>
            <select id="provFree"><option value="true">免费</option><option value="false">付费</option></select>
          </div>
        </div>
        <div class="form-actions" style="margin-top:10px">
          <button class="btn btn-sm btn-primary" onclick="saveProvider()">保存 Provider</button>
          <button class="btn btn-sm" onclick="hideProviderEditor()">取消</button>
          <button class="btn btn-sm" onclick="testProvider()">测试连通性</button>
          <span id="provTestResult" style="font-size:12px;margin-left:8px"></span>
        </div>
      </div>
      <div id="providerPresets" style="display:none;margin-top:14px;border:1px solid var(--border2);border-radius:10px;padding:14px"></div>
    </div>
  </div>
  </div>
</div>

<div id="tab-about" class="tab-panel" style="display:none">
  <div class="large-title">关于</div>
  <div class="large-subtitle">应用信息、使用指南与开源协议</div>

  <div class="section">
    <div class="section-title">应用信息</div>
    <div class="section-body">
      <div style="display:flex;align-items:center;gap:16px;margin-bottom:16px">
        <div style="width:56px;height:56px;border-radius:12px;background:linear-gradient(135deg,#4F46E5,#7C3AED);display:flex;align-items:center;justify-content:center;flex-shrink:0">
          <svg viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="width:28px;height:28px"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>
        </div>
        <div>
          <div style="font-size:20px;font-weight:700;color:var(--text)">Cline2API</div>
          <div style="font-size:13px;color:var(--text2);margin-top:2px">Cline API 反向代理 · 多账号轮询 · 双协议兼容</div>
          <div style="font-size:12px;color:var(--text3);margin-top:4px"><span id="aboutVersion">版本 dev</span> · MIT License · Go 1.25 + Wails v2</div>
        </div>
      </div>
    </div>
  </div>

  <div class="section">
    <div class="section-title">快速上手</div>
    <div class="section-body">
      <div style="display:flex;flex-direction:column;gap:16px">
        <div style="display:flex;gap:12px;align-items:flex-start">
          <div style="width:28px;height:28px;border-radius:50%;background:var(--accent-soft);color:var(--accent);display:flex;align-items:center;justify-content:center;font-weight:700;font-size:14px;flex-shrink:0">1</div>
          <div>
            <div style="font-weight:600;color:var(--text)">添加 Cline 账号</div>
            <div style="font-size:13px;color:var(--text2);margin-top:2px">前往「导入账号」页面，通过 OAuth 登录或手动输入 refreshToken 添加账号。支持批量导入。</div>
          </div>
        </div>
        <div style="display:flex;gap:12px;align-items:flex-start">
          <div style="width:28px;height:28px;border-radius:50%;background:var(--accent-soft);color:var(--accent);display:flex;align-items:center;justify-content:center;font-weight:700;font-size:14px;flex-shrink:0">2</div>
          <div>
            <div style="font-weight:600;color:var(--text)">生成 API Key</div>
            <div style="font-size:13px;color:var(--text2);margin-top:2px">前往「密钥与安全」页面生成密钥。如不配置任何密钥，代理允许匿名访问。</div>
          </div>
        </div>
        <div style="display:flex;gap:12px;align-items:flex-start">
          <div style="width:28px;height:28px;border-radius:50%;background:var(--accent-soft);color:var(--accent);display:flex;align-items:center;justify-content:center;font-weight:700;font-size:14px;flex-shrink:0">3</div>
          <div>
            <div style="font-weight:600;color:var(--text)">配置客户端</div>
            <div style="font-size:13px;color:var(--text2);margin-top:2px">在 Claude Code、Cline 等客户端中设置：</div>
            <div style="font-family:ui-monospace,monospace;font-size:12px;background:var(--surface2);padding:8px 12px;border-radius:6px;margin-top:6px;color:var(--text2)">
              Base URL: http://127.0.0.1:3457/v1<br>
              API Key: &lt;生成的密钥&gt;<br>
              Model: cline-free/glm-5.2
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>

  <div class="section">
    <div class="section-title">功能特性</div>
    <div class="section-body">
      <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:12px;font-size:13px;color:var(--text2)">
        <div>✅ 多账号轮询（轮询/填满/随机）</div>
        <div>✅ OpenAI & Anthropic 双协议</div>
        <div>✅ 429 冷却自动恢复</div>
        <div>✅ 账号导出/导入（跨设备迁移）</div>
        <div>✅ OAuth 系统浏览器登录</div>
        <div>✅ 请求日志与统计</div>
        <div>✅ System Prompt 覆盖</div>
        <div>✅ 跨平台桌面端（Win/Mac/Linux）</div>
      </div>
    </div>
  </div>

  <div class="section">
    <div class="section-title">项目链接</div>
    <div class="section-body">
      <div style="display:flex;flex-direction:column;gap:10px;font-size:14px">
        <div><span style="color:var(--text3);display:inline-block;width:80px">仓库地址</span><a href="#" onclick="openExternal('https://github.com/luawei1/cline2api');return false" style="color:var(--accent);cursor:pointer">github.com/luawei1/cline2api</a></div>
        <div><span style="color:var(--text3);display:inline-block;width:80px">问题反馈</span><a href="#" onclick="openExternal('https://github.com/luawei1/cline2api/issues');return false" style="color:var(--accent);cursor:pointer">github.com/luawei1/cline2api/issues</a></div>
        <div><span style="color:var(--text3);display:inline-block;width:80px">下载更新</span><a href="#" onclick="openExternal('https://github.com/luawei1/cline2api/releases');return false" style="color:var(--accent);cursor:pointer">github.com/luawei1/cline2api/releases</a></div>
        <div><span style="color:var(--text3);display:inline-block;width:80px">开源协议</span>MIT License © 2026 luawei1</div>
      </div>
    </div>
  </div>

  <div class="section">
    <div class="section-title">数据与隐私</div>
    <div class="section-body">
      <div style="font-size:13px;color:var(--text2);line-height:1.8">
        <div>• 本程序仅在本地运行，默认监听 <code style="background:var(--surface2);padding:1px 5px;border-radius:3px;font-size:12px">127.0.0.1:3457</code>，不对外暴露。</div>
        <div>• 账号凭据（refreshToken）存储在数据目录的 <code style="background:var(--surface2);padding:1px 5px;border-radius:3px;font-size:12px">.cline-accounts.json</code>（Docker 中位于 /app/data，可用 CLINE2API_DATA_DIR 配置）中，明文保存，请注意保护。</div>
        <div>• 所有 API 请求通过本机代理转发至 Cline 官方服务器，不经过任何第三方。</div>
        <div>• 关闭窗口即停止服务，无后台驻留进程。</div>
      </div>
    </div>
  </div>
</div>

</div>
</div>
</div>

<div id="toast" class="toast"></div>

<div id="loginOverlay" style="display:none;position:fixed;inset:0;z-index:9999;background:rgba(248,250,252,0.96);align-items:center;justify-content:center">
  <div style="width:min(360px,calc(100vw - 40px));padding:32px;background:var(--surface);border-radius:14px;border:1px solid var(--border2);text-align:center;box-shadow:0 10px 40px rgba(15,23,42,0.12)">
    <h2 style="margin:0 0 6px;font-size:20px;color:var(--text)">Cline2API 管理后台</h2>
    <p style="margin:0 0 22px;color:var(--text2);font-size:13px">该后台已启用访问密码，请输入密码登录</p>
    <input type="password" id="loginPassword" placeholder="访问密码" autocomplete="current-password" style="width:100%;box-sizing:border-box;padding:10px 12px;border-radius:8px;border:1px solid var(--border2);background:var(--surface2);color:var(--text);font-size:14px" onkeydown="if(event.key==='Enter')submitLogin()">
    <button class="btn btn-primary" style="width:100%;margin-top:14px" onclick="submitLogin()">登 录</button>
    <div id="loginError" style="color:var(--red);font-size:13px;margin-top:12px"></div>
  </div>
</div>

<div id="modelSyncOverlay" style="display:none;position:fixed;inset:0;z-index:9998;background:rgba(15,23,42,0.45);align-items:center;justify-content:center">
  <div id="modelSyncModal" style="width:min(480px,calc(100vw - 40px));padding:24px;background:var(--surface);border-radius:14px;border:1px solid var(--border2);box-shadow:0 10px 40px rgba(15,23,42,0.2)"></div>
</div>

<script>

// ===== i18n =====
const I18N = {
  'Cline 代理': 'Cline Proxy',
  '多账号轮询 · 双协议': 'Multi-account rotation · Dual protocol',
  '管理': 'Admin',
  '仪表盘': 'Dashboard',
  '账号管理': 'Accounts',
  '导入账号': 'Import',
  '请求日志': 'Request Logs',
  '设置': 'Settings',
  '关于': 'About',
  '反馈': 'Feedback',
  '查看账号池状态与快捷操作': 'Pool status & quick actions',
  '账号状态': 'Account Status',
  '账号总数': 'Total Accounts',
  '活跃': 'Active',
  '冷却': 'Cooldown',
  '已过期': 'Expired',
  'Token 用量': 'Token Usage',
  '累计输入 Token': 'Total Input Tokens',
  '累计输出 Token': 'Total Output Tokens',
  '累计总 Token': 'Total Tokens',
  '缓存 Token': 'Cached Tokens',
  '快捷操作': 'Quick Actions',
  '添加账号': 'Add Account',
  '刷新全部 Token': 'Refresh All Tokens',
  '从文件导入': 'Import from File',
  '生成 API 密钥': 'Generate API Key',
  '管理 Cline 账号池中的所有账号': 'Manage all accounts in the pool',
  '测试全部': 'Test All',
  '导出': 'Export',
  '添加': 'Add',
  '刷新': 'Refresh',
  '邮箱': 'Email',
  '状态': 'Status',
  '请求': 'Requests',
  '输入': 'Input',
  '输出': 'Output',
  '总 Token': 'Total Tokens',
  '缓存': 'Cache',
  '最后使用': 'Last Used',
  '创建时间': 'Created',
  '操作': 'Actions',
  '加载中...': 'Loading...',
  '通过 OAuth 登录、手动 Token 或批量文件添加账号': 'Add accounts via OAuth login, manual token, or batch file',
  'OAuth 浏览器登录': 'OAuth Browser Login',
  '手动输入 Token': 'Manual Token',
  '批量导入': 'Batch Import',
  '通过浏览器完成 OAuth 认证，支持 Google/GitHub/邮箱登录，自动获取 refreshToken。': 'Complete OAuth in the browser (Google/GitHub/email); refreshToken is fetched automatically.',
  '开始 OAuth 登录': 'Start OAuth Login',
  '等待浏览器授权...': 'Waiting for browser authorization...',
  '点击链接（系统浏览器打开）:': 'Open this link (system browser):',
  '并输入代码:': 'and enter the code:',
  '输入已有的 Cline refreshToken，系统会自动验证并加入池。': 'Paste an existing Cline refreshToken; it will be validated and added.',
  '邮箱（可选，留空自动生成）': 'Email (optional, auto-generated if empty)',
  '批量导入多个账号。支持 JSON 数组或每行一个 token。': 'Import many accounts at once. Supports JSON arrays or one token per line.',
  '导入全部': 'Import All',
  '选择文件': 'Choose File',
  '查看每次请求的 Token 消耗、缓存、耗时与流式速度': 'Token usage, caching, latency & streaming speed per request',
  '时间': 'Time',
  '账号': 'Account',
  '协议': 'Protocol',
  '模型': 'Model',
  '总': 'Total',
  '耗时': 'Duration',
  '加载更多': 'Load More',
  '管理 API 密钥、模型、代理配置与请求头': 'Manage API keys, models, proxy config & headers',
  'API 密钥管理': 'API Keys',
  '生成的密钥可用于客户端访问代理 API（作为 x-api-key 或 Authorization 头）。': 'Generated keys let clients call the proxy API (as x-api-key or Authorization header).',
  '生成新密钥': 'Generate New Key',
  '可用模型': 'Available Models',
  '添加模型': 'Add Model',
  '计费': 'Billing',
  '付费 (pass)': 'Paid (pass)',
  '免费 (free)': 'Free (free)',
  '访问设置': 'Access Settings',
  '监听地址': 'Listen Address',
  '127.0.0.1（仅本机）': '127.0.0.1 (local only)',
  '0.0.0.0（所有网卡）': '0.0.0.0 (all interfaces)',
  '当前地址': 'Current Address',
  '本机 IP（局域网访问地址）': 'Local IPs (LAN access)',
  '管理后台密码（': 'Admin password (',
  '未启用': 'Disabled',
  '保存': 'Save',
  '设置后访问管理后台需输入密码，默认无密码': 'Password required after enabling; none by default',
  '代理配置': 'Proxy Config',
  '默认模型': 'Default Model',
  '轮询策略': 'Rotation Strategy',
  '回退模型链': 'Fallback Model Chain',
  '逗号分隔模型 ID，可用': 'Comma-separated model IDs; you can use ',
  '表示内置免费回退链；留空则自动回退到内置免费链。': ' for the built-in free fallback chain. Leave blank to use it automatically.',
  '轮询 (round_robin)': 'Round robin',
  '填满 (fill)': 'Fill',
  '随机 (random)': 'Random',
  '质量优先 (quality)': 'Quality-first (quality)',
  '引擎版本': 'Engine Version',
  '账号文件': 'Account File',
  '请求头配置（模拟 Cline CLI 发出）': 'Request Headers (mimic Cline CLI)',
  '这些请求头会附加到所有转发给 Cline API 的请求中，以模拟官方客户端行为。': 'Attached to every request forwarded to the Cline API.',
  '请求头': 'Header',
  '值': 'Value',
  '添加请求头': 'Add Header',
  '保存请求头': 'Save Headers',
  '危险操作': 'Danger Zone',
  '删除全部账号': 'Delete All Accounts',
  '删除全部密钥': 'Delete All Keys',
  '应用信息、使用指南与开源协议': 'App info, usage guide & license',
  '应用信息': 'About App',
  'Cline API 反向代理 · 多账号轮询 · 双协议兼容': 'Cline API reverse proxy · multi-account rotation · dual protocol',
  '快速上手': 'Quick Start',
  '添加 Cline 账号': 'Add a Cline account',
  '前往「导入账号」页面，通过 OAuth 登录或手动输入 refreshToken 添加账号。支持批量导入。': 'Go to Import and add via OAuth or a pasted refreshToken. Batch import supported.',
  '生成 API Key': 'Generate an API Key',
  '前往「密钥与安全」页面生成密钥。如不配置任何密钥，代理允许匿名访问。': 'Generate a key in Keys & Security. With no keys, the proxy allows anonymous access.',
  '配置客户端': 'Configure your client',
  '在 Claude Code、Cline 等客户端中设置：': 'Configure in clients like Claude Code or Cline:',
  '功能特性': 'Features',
  '✅ 多账号轮询（轮询/填满/随机）': '✅ Multi-account rotation (round robin / fill / random)',
  '✅ OpenAI & Anthropic 双协议': '✅ OpenAI & Anthropic dual protocol',
  '✅ 429 冷却自动恢复': '✅ Auto-recovery from 429 cooldown',
  '✅ 账号导出/导入（跨设备迁移）': '✅ Account export/import (device migration)',
  '✅ OAuth 系统浏览器登录': '✅ OAuth login via system browser',
  '✅ 请求日志与统计': '✅ Request logs & stats',
  '✅ System Prompt 覆盖': '✅ System Prompt override',
  '✅ 跨平台桌面端（Win/Mac/Linux）': '✅ Cross-platform desktop (Win/Mac/Linux)',
  '项目链接': 'Links',
  '仓库地址': 'Repository',
  '问题反馈': 'Issues',
  '下载更新': 'Releases',
  '开源协议': 'License',
  '数据与隐私': 'Data & Privacy',
  '• 本程序仅在本地运行，默认监听': '• Runs locally, listens on',
  '，不对外暴露。': ', not exposed externally.',
  '• 账号凭据（refreshToken）存储在数据目录的': '• Credentials (refreshToken) are stored in plaintext in the file ',
  '（Docker 中位于 /app/data，可用 CLINE2API_DATA_DIR 配置）中，明文保存，请注意保护。': ' (in Docker: /app/data, configurable via CLINE2API_DATA_DIR); keep them safe.',
  '• 所有 API 请求通过本机代理转发至 Cline 官方服务器，不经过任何第三方。': '• All API requests are proxied to Cline\'s official servers, never through third parties.',
  '• 关闭窗口即停止服务，无后台驻留进程。': '• Closing the window stops the service; no background process.',
  'Cline2API 管理后台': 'Cline2API Admin',
  '该后台已启用访问密码，请输入密码登录': 'Password protection is enabled. Enter the password to continue.',
  '登 录': 'Sign In',
  '即将恢复': 'recovering soon',
  '需要登录': 'Login required',
  '登录失败': 'Login failed',
  '网络错误，请重试': 'Network error, please retry',
  '已在系统浏览器中打开': 'Opened in system browser',
  '打开失败: ': 'Open failed: ',
  '测试': 'Test',
  '重置': 'Reset',
  '删除': 'Delete',
  '从未使用': 'Never used',
  '加载账号失败: ': 'Failed to load accounts: ',
  ' · 输入 ': ' · Input ',
  ' · 输出 ': ' · Output ',
  ' · 缓存 ': ' · Cached ',
  ' · 总 ': ' · Total ',
  '输入 ': 'Input ',
  '测试成功：': 'Test OK: ',
  '测试失败：': 'Test failed: ',
  '未知错误': 'Unknown error',
  '测试失败: ': 'Test failed: ',
  '正在测试全部账号，请稍候...': 'Testing all accounts, please wait...',
  '全部测试通过：': 'All tests passed: ',
  ' 个账号正常': ' accounts OK',
  '测试完成：': 'Tests finished: ',
  ' 成功 / ': ' OK / ',
  ' 失败 · ': ' failed · ',
  '确定删除此账号？': 'Delete this account?',
  '账号已删除': 'Account deleted',
  '删除失败: ': 'Delete failed: ',
  '确定重置此账号？将恢复为活跃状态并刷新 Token，保留历史统计。': 'Reset this account? It will become active, tokens refreshed, history kept.',
  '账号已重置': 'Account reset',
  '重置失败: ': 'Reset failed: ',
  '⚠️ 确定删除所有账号？不可撤销！': '⚠️ Delete ALL accounts? This cannot be undone!',
  '全部账号已删除': 'All accounts deleted',
  '全部 Token 已刷新': 'All tokens refreshed',
  '刷新失败: ': 'Refresh failed: ',
  '正在连接 WorkOS...': 'Connecting to WorkOS...',
  '请在浏览器中打开链接并输入代码': 'Open the link in your browser and enter the code',
  '账号添加成功！': 'Account added!',
  '失败: ': 'Failed: ',
  'OAuth 失败': 'OAuth failed',
  '错误: ': 'Error: ',
  'OAuth 失败: ': 'OAuth failed: ',
  '请输入 refreshToken': 'Enter a refreshToken',
  '账号添加成功: ': 'Account added: ',
  '添加失败: ': 'Add failed: ',
  '账号已导出': 'Accounts exported',
  '导出失败: ': 'Export failed: ',
  '已导出 ': 'Exported ',
  '导出选中': 'Export selected',
  '删除选中': 'Delete selected',
  '导出全部': 'Export all',
  '请先勾选要导出的账号': 'Select accounts to export first',
  '请先勾选要删除的账号': 'Select accounts to delete first',
  '确认删除选中的 ': 'Confirm deleting ',
  ' 个账号？此操作不可恢复': ' selected accounts? This cannot be undone',
  '删除中...': 'Deleting...',
  '已删除 ': 'Deleted ',
  ' 个，失败 ': ' accounts, ',
  ' 个失败': ' failed',
  '共': 'Total ',
  '个账号 · 每页': ' accounts · per page ',
  '全选本页': 'Select all on this page',
  '已下架': 'Delisted',
  '匹配': 'Match ',
  '没有匹配的账号': 'No matching accounts',
  '请先勾选要重置的账号': 'Select accounts to reset first',
  '确认重置选中的 ': 'Confirm resetting ',
  ' 个账号？（清除冷却与失败计数）': ' selected accounts? (clears cooldowns and failure counts)',
  '重置中...': 'Resetting...',
  '已重置 ': 'Reset ',
  '免费模型': 'Free models',
  '付费模型': 'Paid models',
  '（免费）': ' (free)',
  '（付费）': ' (paid)',
  '（当前）': ' (current)',
  '移除（已下架模型支持手动移除）': 'Remove (delisted models can be removed manually)',
  '已下架（保留在列表，标记显示）': 'Delisted (kept in list, marked)',
  '已从上游官方列表移除，实测可能仍可用；同步只标记不删除，上游报模型不存在时自动清理': 'Removed from the upstream official list but may still work; sync only marks it, and it is auto-cleaned when the upstream reports the model no longer exists',
  '请输入账号数据': 'Enter account data',
  '导入完成': 'Import done',
  '导入失败: ': 'Import failed: ',
  '导入了 ': 'Imported ',
  ' 个账号': ' accounts',
  '点击复制': 'Click to copy',
  '密钥已生成': 'Key generated',
  '生成失败: ': 'Generate failed: ',
  '确定删除此密钥？': 'Delete this key?',
  '密钥已删除': 'Key deleted',
  '确定删除所有 API 密钥？': 'Delete ALL API keys?',
  '全部密钥已删除': 'All keys deleted',
  '已复制到剪贴板': 'Copied to clipboard',
  '配置已更新': 'Config updated',
  '更新失败: ': 'Update failed: ',
  '已保存，正在重启监听...': 'Saved, restarting listener...',
  '监听已切换，请通过 ': 'Listener switched, use ',
  ' 访问管理后台': ' to access the admin panel',
  '保存失败: ': 'Save failed: ',
  '密码至少 4 位': 'Password must be at least 4 characters',
  '密码已设置，后台需要重新登录': 'Password set; admin requires re-login',
  '已清除密码': 'Password cleared',
  '存在有值无键的行，已忽略': 'Rows with a value but no key were ignored',
  '请求头已保存': 'Headers saved',
  '加载失败': 'Failed to load',
  '请输入模型 ID': 'Enter a model ID',
  '模型已添加: ': 'Model added: ',
  '确认删除模型 ': 'Delete model ',
  '模型已删除: ': 'Model deleted: ',
  '版本 ': 'Version ',
  '（本机网卡）': ' (local NIC)',
  '已启用': 'Enabled',
  '完成': 'Done',
  '失败': 'Failed',
  'Token 未知': 'Token unknown',
  '加载日志失败: ': 'Failed to load logs: ',
  '测试中...': 'Testing...',
  '启动中...': 'Starting...',
  '从 Cline 同步模型': 'Sync Models from Cline',
  '模型同步': 'Model Sync',
  '上次同步': 'Last synced',
  '从未同步': 'Never synced',
  '新增模型': 'Added models',
  '移除模型': 'Removed models',
  '模型无变化': 'No model changes',
  '模型列表已更新': 'Model list updated',
  '上下文窗口 / 最大输出 (token)，点击⚙修改': 'Context window / max output (tokens), click ⚙ to edit',
  '上下文窗口 (token)': 'Context window (tokens)',
  '最大输出 (token)': 'Max output (tokens)',
  '已保存': 'Saved',
  '保存失败': 'Save failed',
  '同步中...': 'Syncing...',
  '暂无模型': 'No models',
  '模型统计': 'Model Usage',
  '模型用量统计': 'Model Usage Stats',
  '通道': 'Channel',
  '请求数': 'Requests',
  '总计': 'Total',
  '今日': 'Today',
  '累计': 'All time',
  '按模型统计（仅免费模型）': 'Per-model usage (free models)',
  '免费模型': 'Free models',
  '展开': 'Expand',
  '收起': 'Collapse',
  '模型冷却中': 'Model cooling',
  '模型冷却中，约 ': 'Model cooling, ~',
  '后释放': ' until release',
  '暂无数据': 'No data yet',
  // opencode 免费模型
  '从 opencode 同步模型': 'Sync Models from opencode',
  'opencode 模型列表已更新': 'opencode model list updated',
  'opencode 免费模型 · 今日用量': 'opencode Free Models · Today',
  '今日请求数': 'Requests today',
  '今日输入 Token': 'Input tokens today',
  '今日输出 Token': 'Output tokens today',
  '今日总 Token': 'Total tokens today',
  '启用 opencode': 'Enable opencode',
  '启用': 'Enabled',
  '停用': 'Disabled',
  'API Key': 'API Key',
  'Base URL': 'Base URL',
  '最大并发': 'Max concurrency',
  '重试次数': 'Retries',
  '故障转移': 'Failover',
  '开启（连续失败后暂走 Cline 池）': 'On (temporarily route to Cline pool after consecutive failures)',
  '关闭': 'Off',
  '失败阈值（次）': 'Failure threshold',
  '转移窗口（分钟）': 'Failover window (min)',
  '自动上下文压缩': 'Auto context compaction',
  '开启（超限自动摘要）': 'On (auto summary when over limit)',
  '压缩缓冲 Token': 'Compaction buffer (tokens)',
  '尾部保留 Token': 'Tail keep tokens',
  '摘要最大 Token': 'Max summary tokens',
  '保存 opencode 配置': 'Save opencode Config',
  '手动保存 opencode 配置': 'Manually save opencode Config',
  '上述字段改动后会自动保存，也可点按钮手动触发。': 'Fields above are auto-saved on change; you can also trigger a save manually with the button.',
  'opencode 配置已保存': 'OpenCode config saved',
  'opencode 出口代理': 'OpenCode Egress Proxies',
  '发往 opencode 的请求可经代理池轮询出口；命中限流时冷却当前出口并自动跳过。支持 http / https / socks5 / socks5h，每行一个，如 ': 'Requests to opencode can egress through a rotating proxy pool; the current proxy is cooled down and skipped on rate limits. Supports http / https / socks5 / socks5h, one per line, e.g. ',
  '发往 opencode 的请求可经代理池轮询出口；命中限流时冷却当前出口并自动跳过。支持 http / https / socks5 / socks5h，每行一个，如': 'Requests to opencode can egress through a rotating proxy pool; the current proxy is cooled down and skipped on rate limits. Supports http / https / socks5 / socks5h, one per line, e.g.',
  'opencode 免费模型': 'opencode Free Models',
  '＋ 添加 Provider': '＋ Add Provider',
  '名称 *': 'Name *',
  '免费': 'Free',
  '付费': 'Paid',
  '版本 ': 'Version ',
  '版本 dev': 'Version dev',
  'JSON 数组格式：[{"refreshToken":"...","email":"..."}]': 'JSON array format: [{"refreshToken":"...","email":"..."}]',
  'API Key: <生成的密钥>': 'API Key: <generated key>',
  '代理策略': 'Proxy strategy',
  '自定义 Provider（OpenAI 兼容）': 'Custom Providers (OpenAI-compatible)',
  '批量编辑': 'Bulk Edit',
  '批量编辑（每行 Key: Value）': 'Bulk edit (one Key: Value per line)',
  '应用并保存': 'Apply & Save',
  '名称': 'Name',
  '取消': 'Cancel',
  '保存 Provider': 'Save Provider',
  '添加 Provider': 'Add Provider',
  '本地服务器可留空）': 'leave empty on local server)',
  '超时(秒)': 'Timeout (s)',
  '优先级（小=优先）': 'Priority (lower = first)',
  '分钟请求上限（0=不限）': 'RPM limit (0 = unlimited)',
  '每日请求上限（0=不限）': 'Daily request limit (0 = unlimited)',
  '配额提示：填入免费层限额后，路由会在派发前自动避开打满额度的上游，改用组内其他 provider，不再拿 429 试错': 'Quota hint: set the free-tier cap and routing avoids exhausted upstreams before dispatch, failing over to other providers in the group instead of discovering 429s by trial',
  '免费来源（供统计）': 'Free source (for stats)',
  '模型 ID（逗号分隔，如 z-ai/glm-5.3-flash, deepseek/deepseek-v4-flash）': 'Model IDs (comma-separated, e.g. z-ai/glm-5.3-flash, deepseek/deepseek-v4-flash)',
  '自定义请求头（每行 Key: Value，如 HTTP-Referer: https://mysite.com）': 'Custom headers (one Key: Value per line, e.g. HTTP-Referer: https://mysite.com)',
  '请求头（每行一个，格式 Key: Value）': 'Headers (one per line, format Key: Value)',
  '保存 opencode 请求头': 'Save opencode Headers',
  'opencode 请求头已保存': 'opencode headers saved',
  '附加到发往 opencode zen 的请求。特殊值：': 'Attached to requests sent to opencode zen. Special values: ',
  '附加到发往 opencode zen 的请求。特殊值：$session / $request / $project / $client 会注入每请求的动态身份；留空删除该头。': 'Attached to requests sent to opencode zen. Special values: $session / $request / $project / $client inject per-request dynamic identity; an empty value deletes the header.',
  '接入任意 OpenAI 兼容上游（OpenRouter / Groq / Cerebras / Gemini / Mistral / Together / 自建 vLLM 等）。模型命中自定义 Provider 时优先走该上游，失败自动按回退链降级，最终兜底 Cline 池。': 'Connect any OpenAI-compatible upstream (OpenRouter / Groq / Cerebras / Gemini / Mistral / Together / self-hosted vLLM, etc.). Matching models route to the provider first; failures fall back along the chain and finally to the Cline pool.',
  'opencode 请求头（模拟官方客户端）': 'opencode Headers (mimic official client)',
  '粘贴 refreshToken': 'Paste refreshToken',
  '如 deepseek/deepseek-v4-flash': 'e.g. deepseek/deepseek-v4-flash',
  '留空保存 = 清除密码': 'leave empty to save = clear password',
  '多账号轮询（轮询/填满/随机）': 'Multi-account rotation (round-robin/fill/random)',
  'OpenAI & Anthropic 双协议': 'OpenAI & Anthropic dual protocol',
  '429 冷却自动恢复': 'Automatic 429 cooldown recovery',
  '账号导出/导入（跨设备迁移）': 'Account export/import (cross-device migration)',
  'OAuth 系统浏览器登录': 'OAuth via system browser',
  '请求日志与统计': 'Request logs & stats',
  'System Prompt 覆盖': 'System Prompt override',
  '跨平台桌面端（Win/Mac/Linux）': 'Cross-platform desktop (Win/Mac/Linux)',
  '⚠️ 当前监听非本机回环地址（0.0.0.0 或局域网 IP），管理后台无鉴权，局域网内任何设备都可访问。请确认网络环境安全，或配合防火墙限制端口。': '⚠️ Listening on a non-loopback address (0.0.0.0 or LAN IP) exposes the admin panel without authentication to the whole network. Ensure the network is trusted or restrict the port via firewall.',
  '概览': 'Overview',
  '账号': 'Accounts',
  '配置': 'Configuration',
  '系统': 'System',
  '路由与模型': 'Routing & Models',
  '上游服务': 'Upstreams',
  '密钥与安全': 'Keys & Security',
  '默认模型、轮询策略、回退链与可用模型': 'Default model, rotation strategy, fallback chain & models',
  'opencode zen、Cline 出口代理、请求头与自定义 Provider': 'opencode zen, Cline egress proxies, headers & custom providers',
  'Cline 请求头': 'Cline Request Headers',
  'opencode 请求头': 'OpenCode Request Headers',
  '请求头': 'Headers',
  '自定义 Provider': 'Providers',
  'API 密钥、访问控制与危险操作': 'API keys, access control & danger zone',
  '从预设添加（免费源）': 'Add from presets (free tiers)',
  '测试连通性': 'Test connection',
  '暂无自定义 Provider': 'No custom providers yet',
  '出口冷却状态': 'Egress cooldowns',
  '代理列表': 'Proxy list',
  '无冷却': 'None cooling',
  '已停用': 'Disabled',
  '故障转移中（opencode 暂不可用，请求走 Cline 池）': 'Failover active (opencode unavailable, requests routed to Cline pool)',
  '正常': 'Healthy',
  '已同步模型': 'Models synced',
  '接入 opencode（zen）免费模型。按请求中的模型名自动分流：免费模型走 opencode 上游，付费模型直接拒绝，其余走 Cline 账号池。': 'Integrates opencode (zen) free models. Requests are routed automatically by model name: free models go to the opencode upstream, paid models are rejected, everything else goes to the Cline account pool.',
  // Cline 出口代理
  'Cline 出口代理': 'Cline Egress Proxies',
  '国内直连 Cline 上游会被跨区限制；配置后所有发往 Cline 的请求（对话、登录/令牌刷新、模型同步）经代理池轮询出去。支持 http / https / socks5 / socks5h，每行一个，如 ': 'Cline upstream is region-restricted from mainland China; once configured, all requests to Cline (chat, login/token refresh, model sync) egress through a rotating proxy pool. Supports http / https / socks5 / socks5h, one per line, e.g. ',
  '。回环 / 内网地址（本机 Ollama 等自定义 Provider）始终直连；未配置时依次尝试环境变量代理、直连。': '. Loopback / private addresses (local Ollama and other custom providers) always connect directly; without config, env proxies then direct connection are tried.',
  '保存 Cline 代理': 'Save Cline proxies',
  'Cline 代理配置已保存': 'Cline proxy config saved',
  // 模型分组
  'opencode · 免费模型': 'opencode · Free Models',
  'opencode · 付费模型': 'opencode · Paid Models',
  'Cline · 免费模型': 'Cline · Free Models',
  'Cline · 付费模型': 'Cline · Paid Models',
  '用户自定义': 'User Custom',
  '点击展开/折叠': 'Click to expand/collapse',
  // 模型管理（提供商分组 + 启停开关）
  '路由配置': 'Routing',
  '模型管理': 'Models',
  '管理从 Cline / opencode 同步的模型与自定义模型。停用的模型不会出现在客户端模型列表（/v1/models）中，直接请求也会被拒绝。': 'Manage models synced from Cline / opencode and custom models. Disabled models are hidden from the client model list (/v1/models) and rejected when requested.',
  '全部提供商': 'All providers',
  '全部计费': 'All billing',
  '全部状态': 'All statuses',
  '全部启用': 'Enable all',
  '全部停用': 'Disable all',
  '无匹配模型': 'No matching models',
  '点击启用该模型': 'Click to enable this model',
  '点击停用该模型': 'Click to disable this model',
  '自定义': 'Custom',
  '其他': 'Other',
  '个模型': ' models',
  '＋ 添加模型': '＋ Add Model',
  // 模型组合（Combo）
  '模型组合': 'Model Combos',
  '＋ 新建组合': '＋ New Combo',
  '把多个模型按优先级编排成组合（Combo）：命中组合时按顺序依次尝试，一个模型失败（限流/不可用）就自动切到下一个。停用的条目会被直接跳过。': 'Arrange several models into a priority-ordered combo. A matched combo is tried in order; when one model fails (rate-limited/unavailable) it automatically moves to the next. Disabled entries are skipped.',
  '组合 ID': 'Combo ID',
  '组合 ID *': 'Combo ID *',
  '描述': 'Description',
  '可选': 'optional',
  '分类': 'Category',
  'coding（可选）': 'coding (optional)',
  '紧急回退': 'Emergency fallback',
  '允许跳出组合回退': 'Allow fallback outside the combo',
  '开启「紧急回退」后，仅当组合内所有条目都失败时，才允许继续回退到配置的全局回退链 / 内置免费链；关闭则组合内失败即结束。': 'When "Emergency fallback" is on, only after every entry in the combo fails is it allowed to fall through to the configured global fallback chain / built-in free chain. When off, a combo failure ends the request.',
  '模型条目（按优先级从上到下）': 'Model entries (priority, top to bottom)',
  '＋ 添加条目': '＋ Add entry',
  '优先级预览': 'Priority preview',
  '（暂无启用的条目）': '(no enabled entries)',
  '仅显示已启用的条目，停用的条目会被跳过。': 'Only enabled entries are shown; disabled entries are skipped.',
  '保存组合': 'Save Combo',
  '组合已保存': 'Combo saved',
  '暂无模型组合': 'No model combos yet',
  '条目': 'Entries',
  '编辑': 'Edit',
  '克隆': 'Clone',
  '已克隆': 'Cloned',
  '克隆失败: ': 'Clone failed: ',
  '确定删除该组合？': 'Delete this combo?',
  '未选择模型': 'No model selected',
  '目录中未找到': 'Not in catalog',
  '模型已停用': 'Model disabled',
  '尚未添加条目，点击下方「添加条目」开始。': 'No entries yet. Click "Add entry" below to start.',
  '启用/停用该条目': 'Enable/disable this entry',
  '上移': 'Move up',
  '下移': 'Move down',
  '移除': 'Remove',
  '— 选择模型 —': '— Select model —',
  '上游': 'Upstream',
  '自动': 'Auto',
  '已使用': 'in use',
  '请填写组合 ID': 'Enter a combo ID',
  '请填写名称': 'Enter a name',
  '该 ID 为保留别名，请更换': 'This ID is a reserved alias; choose another',
  '条目中存在重复模型: ': 'Duplicate model in entries: ',
  // 请求日志：组合与尝试链路
  '组合': 'Combo',
  '回退': 'Fallback',
  '成功': 'Success',
  '跳过': 'Skipped',
  '尝试链路': 'Attempt trace',
  '点击查看尝试链路': 'Click to view the attempt trace',
  '请求: ': 'Requested: ',
  // 模型管理：两级分组 + 免费导向批量开关
  '全部来源': 'All sources',
  'Cline': 'Cline',
  'Zen (opencode)': 'Zen (opencode)',
  '启用全部免费': 'Enable all free',
  '停用全部付费': 'Disable all paid',
  '确定启用全部免费模型？': 'Enable all free models?',
  '确定停用全部付费模型？': 'Disable all paid models?',
  '没有符合条件的模型': 'No matching models',
};
let LANG = 'zh';
const LC = () => LANG === 'en' ? 'en-US' : 'zh-CN';
function detectLang(){
  try { const m = document.cookie.match(/cline_admin_lang=(zh|en)/); if (m) return m[1]; } catch(e){}
  try { const l = localStorage.getItem('cline_admin_lang'); if (l==='en'||l==='zh') return l; } catch(e){}
  return (navigator.language||'').toLowerCase().startsWith('zh') ? 'zh' : 'en';
}
function t(s){ if (LANG==='en' && s && I18N[s]) return I18N[s]; return s; }
// 反向映射：英文 → 中文（用于切回中文时还原静态文本）
const I18N_REV = Object.keys(I18N).reduce((acc, k) => { acc[I18N[k]] = k; return acc; }, {});
function applyLang(){
  if (LANG === 'en') {
    document.title = 'Cline Proxy Admin';
  } else {
    document.title = 'Cline 代理管理面板';
  }
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  const nodes = [];
  while (walker.nextNode()) nodes.push(walker.currentNode);
  const dict = (LANG === 'en') ? I18N : I18N_REV;
  for (const n of nodes){
    const txt = n.nodeValue;
    if (!txt) continue;
    const trimmed = txt.trim();
    if (trimmed && dict[trimmed] && dict[trimmed] !== trimmed){
      n.nodeValue = txt.replace(trimmed, dict[trimmed]);
    }
  }
  document.querySelectorAll('[title]').forEach(el=>{
    const k=(el.getAttribute('title')||'').trim();
    if (k && dict[k]) el.setAttribute('title', dict[k]);
  });
  _('langZh').classList.toggle('active', LANG === 'zh');
  if (_('langEn')) _('langEn').classList.toggle('active', LANG === 'en');
}
function setLang(l){
  LANG = l;
  try { document.cookie = 'cline_admin_lang='+l+';path=/admin;max-age=31536000'; } catch(e){}
  try { localStorage.setItem('cline_admin_lang', l); } catch(e){}
  document.documentElement.lang = (l==='en')?'en':'zh-CN';
  // 即时切换：静态文本 + title 属性翻译 + 重新渲染动态数据，不整页刷新
  applyLang();
  loadStats();
  loadAccounts();
  loadRequestLogs(true);
  if (_('modelsList')) loadModels();
  if (_('settingDefModel')) loadConfig();
  toast(l === 'en' ? 'Language switched to English' : '语言已切换为中文', 'success');
}
LANG = detectLang();
document.documentElement.lang = (LANG==='en')?'en':'zh-CN';
// ===== /i18n =====
const API = '/admin/api';

const _ = id => document.getElementById(id);
const esc = s => { const d=document.createElement('div'); d.textContent=s||''; return d.innerHTML; };
// 属性上下文转义：esc 不处理引号，注入到 onclick / value 等属性时需额外转义引号
const escAttr = s => esc(s).replace(/"/g, '&quot;').replace(/'/g, '&#39;');

const ACTIVE_TAB_KEY = 'cline_admin_tab';

function getActiveTab(){
  try { const h = location.hash.replace(/^#/,''); if (h) return h; } catch(e){}
  try { const v = sessionStorage.getItem(ACTIVE_TAB_KEY); if (v) return v; } catch(e){}
  return 'dashboard';
}
function setActiveTab(name){
  try { sessionStorage.setItem(ACTIVE_TAB_KEY, name); } catch(e){}
}
const formatNumber = n => new Intl.NumberFormat(LC()).format(n || 0);
const formatTokenCount = n => {
  const value = Number(n) || 0;
  if (value < 1000) return String(value);
  const units = [['B', 1e9], ['M', 1e6], ['K', 1e3]];
  for (const [unit, size] of units) {
    if (value >= size) return (value / size).toFixed(1).replace(/\.0$/, '') + unit;
  }
  return String(value);
};

function toast(msg, t) {
  const el = _('toast');
  el.textContent = msg;
  el.className = 'toast ' + t + ' show';
  setTimeout(() => el.classList.remove('show'), 3500);
}

// 格式化冷却倒计时：返回紧凑的 "3.2h" 格式
function formatCooldown(isoTime) {
  const until = new Date(isoTime);
  const diff = until - new Date();
  if (diff <= 0) return t('即将恢复');
  const hours = diff / 3600000;
  if (hours < 1) return Math.ceil(diff / 60000) + 'm';
  return hours.toFixed(1) + 'h';
}

// ========== 导航 ==========
function applyTab(name){
  // 非法/未知标签页（如手改 URL）回退到 dashboard，避免空白页
  if (!name || !_('tab-' + name)) name = 'dashboard';
  document.querySelectorAll('.nav-item').forEach(e => e.classList.toggle('active', e.dataset.tab === name));
  document.querySelectorAll('.tab-panel').forEach(e => e.style.display = 'none');
  const panel = _('tab-' + name);
  if (panel) panel.style.display = 'block';
  try { history.replaceState(null, '', '#' + name); } catch(e){}
  setActiveTab(name);
  if (name === 'dashboard') { applyLang(); loadStats(); loadAccounts(); }
  else if (name === 'accounts') loadAccounts();
  else if (name === 'logs') loadRequestLogs(true);
  else if (name === 'routing') { loadModels().then(() => { loadConfig(); if (routingActiveSub() === 'combos') loadCombos(); }); }
  else if (name === 'upstreams') { loadOcConfig(); loadClineProxyConfig(); loadZenHeaders(); loadProviders(); }
  else if (name === 'security') { loadKeys(); loadConfig(); }
}

function switchTab(name) { applyTab(name); }

document.querySelectorAll('.nav-item').forEach(el => {
  el.addEventListener('click', () => {
    if (el.classList.contains('active')) return;
    applyTab(el.dataset.tab);
  });
});

// 从 URL/SessionStorage 恢复上次打开的标签页（统一在底部初始化时执行）

// ========== 侧栏排序（拖拽） ==========
const NAV_ORDER_KEY = 'cline_admin_nav_order';
function applyNavOrder() {
  let order = [];
  try { order = JSON.parse(localStorage.getItem(NAV_ORDER_KEY) || '[]'); } catch(e) {}
  if (!Array.isArray(order) || !order.length) return;
  const nav = document.querySelector('.nav-section').parentElement;
  const items = {};
  nav.querySelectorAll('.nav-item').forEach(el => { items[el.dataset.tab] = el; });
  // 重排：按保存的顺序把条目移回其分区末尾，未知 tab 忽略
  order.forEach(tab => {
    const el = items[tab];
    if (!el) return;
    const sec = el.closest('.nav-section');
    sec.appendChild(el);
  });
}
function saveNavOrder() {
  const order = Array.from(document.querySelectorAll('.nav-item')).map(el => el.dataset.tab);
  try { localStorage.setItem(NAV_ORDER_KEY, JSON.stringify(order)); } catch(e) {}
}
function initNavDrag() {
  let dragged = null;
  document.querySelectorAll('.nav-item').forEach(el => {
    el.draggable = true;
    el.addEventListener('dragstart', e => { dragged = el; el.classList.add('dragging'); try { e.dataTransfer.setData('text/plain', el.dataset.tab); } catch(err){} e.dataTransfer.effectAllowed = 'move'; });
    el.addEventListener('dragend', () => { el.classList.remove('dragging'); document.querySelectorAll('.nav-item').forEach(x => x.classList.remove('drop-target')); dragged = null; });
    el.addEventListener('dragover', e => {
      e.preventDefault();
      if (!dragged || dragged === el) return;
      e.dataTransfer.dropEffect = 'move';
      document.querySelectorAll('.nav-item').forEach(x => x.classList.remove('drop-target'));
      el.classList.add('drop-target');
    });
    el.addEventListener('dragleave', () => el.classList.remove('drop-target'));
    el.addEventListener('drop', e => {
      e.preventDefault();
      if (!dragged || dragged === el) return;
      const sec = el.closest('.nav-section');
      const rect = el.getBoundingClientRect();
      const after = (e.clientY - rect.top) > rect.height / 2;
      sec.insertBefore(dragged, after ? el.nextSibling : el);
      dragged.classList.remove('dragging');
      document.querySelectorAll('.nav-item').forEach(x => x.classList.remove('drop-target'));
      saveNavOrder();
    });
  });
}
initNavDrag();
applyNavOrder();

// ========== 页内子导航（设置页顶部导航栏，划分功能子页） ==========
const SUBTAB_KEY_PREFIX = 'cline_admin_subtab_';
function switchPageSubTab(page, sub, skipSave) {
  const nav = document.querySelector('.page-subtabs[data-page="' + page + '"]');
  if (!nav) return;
  nav.querySelectorAll('.subtab').forEach(e => e.classList.toggle('active', e.dataset.sub === sub));
  const panel = nav.closest('.tab-panel');
  if (panel) {
    panel.querySelectorAll(':scope > .page-subpage').forEach(g => {
      g.style.display = (g.dataset.subpage === sub) ? '' : 'none';
    });
  }
  if (!skipSave) { try { localStorage.setItem(SUBTAB_KEY_PREFIX + page, sub); } catch(e) {} }
  if (!skipSave) onPageSubTabShown(page, sub);
}

// 子页显示时的按需加载钩子（用户点击切换时触发；首次进入由 applyTab 处理）
function onPageSubTabShown(page, sub) {
  if (page === 'routing' && sub === 'combos') loadCombos();
}

// 读取 routing 页当前激活的子页名（用于顶层标签切换后按需加载）
function routingActiveSub() {
  const nav = document.querySelector('.page-subtabs[data-page="routing"]');
  const el = nav && nav.querySelector('.subtab.active');
  return el ? el.dataset.sub : '';
}
document.querySelectorAll('.page-subtabs').forEach(nav => {
  nav.querySelectorAll('.subtab').forEach(el => {
    el.addEventListener('click', () => switchPageSubTab(nav.dataset.page, el.dataset.sub));
  });
  // 恢复上次选中的子页
  let saved = null;
  try { saved = localStorage.getItem(SUBTAB_KEY_PREFIX + nav.dataset.page); } catch(e) {}
  if (saved && saved !== nav.querySelector('.subtab.active')?.dataset.sub && nav.querySelector('.subtab[data-sub="' + saved + '"]')) {
    switchPageSubTab(nav.dataset.page, saved, true);
  }
});

// 导入子标签
document.querySelectorAll('#importTabs .tab').forEach(el => {
  el.addEventListener('click', () => {
    document.querySelectorAll('#importTabs .tab').forEach(e => e.classList.remove('active'));
    el.classList.add('active');
    document.querySelectorAll('#import-oauth,#import-token,#import-batch').forEach(e => e.classList.remove('active'));
    _('import-' + el.dataset.tab).classList.add('active');
  });
});

// ========== API 请求 ==========
async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
  const res = await fetch(API + path, opts);
  if (res.status === 401) {
    showLogin();
    throw new Error(t('需要登录'));
  }
  const data = await res.json();
  if (!data.success && data.error) throw new Error(data.error);
  return data;
}

// ========== 后台登录 ==========
function showLogin() {
  const ov = _('loginOverlay');
  if (ov && ov.style.display !== 'flex') {
    ov.style.display = 'flex';
    setTimeout(() => _('loginPassword').focus(), 50);
  }
}

async function submitLogin() {
  const pwd = _('loginPassword').value;
  if (!pwd) return;
  _('loginError').textContent = '';
  try {
    const res = await fetch(API + '/login', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: pwd })
    });
    const data = await res.json();
    if (res.ok && data.success) {
      location.reload();
    } else {
      _('loginError').textContent = data.error || t('登录失败');
      _('loginPassword').value = '';
    }
  } catch (e) { _('loginError').textContent = t('网络错误，请重试'); }
}

// 用系统默认浏览器打开外部链接（桌面 WebView 内导航不会跳外部浏览器，需走后端）
async function openExternal(url) {
  try {
    await api('GET', '/open-external?url=' + encodeURIComponent(url));
    toast(t('已在系统浏览器中打开'), 'success');
  } catch (e) { toast(t('打开失败: ') + e.message, 'error'); }
}

// ========== 仪表盘 ==========
async function loadStats() {
  try {
    const d = await api('GET', '/stats');
    const s = d.data;
    _('statTotal').textContent = s.total;
    _('statActive').textContent = s.active;
    _('statCooldown').textContent = s.cooldown;
    _('statExpired').textContent = s.expired;
    _('statPromptTokens').textContent = formatTokenCount(s.promptTokens);
    _('statCompletionTokens').textContent = formatTokenCount(s.completionTokens);
    _('statTotalTokens').textContent = formatTokenCount(s.totalTokens);
    _('statCachedTokens').textContent = formatTokenCount(s.cachedTokens);
    const oc = s.opencodeToday || {};
    _('statOcRequests').textContent = oc.requests != null ? oc.requests : '-';
    _('statOcInputTokens').textContent = formatTokenCount(oc.inputTokens || 0);
    _('statOcOutputTokens').textContent = formatTokenCount(oc.outputTokens || 0);
    _('statOcTotalTokens').textContent = formatTokenCount(oc.totalTokens || 0);
    lastModelUsage = s.modelUsage || [];
    renderModelUsage();
    if (s.version) _('settingVersion').value = s.version;
    // 设置项由 /config 返回为准；这里仅在用户未编辑时回填，避免轮询清掉正在输入的内容
    const fillIfIdle = (id, val) => { const el = _(id); if (el && document.activeElement !== el && !el.dataset.dirty) el.value = val; };
    if (s.strategy) fillIfIdle('settingStrategy', s.strategy);
    fillIfIdle('settingModelChain', (s.modelChain || []).join(', '));
  } catch (e) { /* ignore */ }
}

// ========== 模型用量统计 ==========
let lastModelUsage = [];
let modelUsageRange = 'today';

function switchModelUsageRange(range) {
  modelUsageRange = range;
  renderModelUsage();
}

function renderModelUsage() {
  const tbody = _('modelUsageBody');
  if (!tbody) return;
  _('modelUsageBtnToday').style.borderColor = modelUsageRange === 'today' ? 'var(--accent)' : '';
  _('modelUsageBtnToday').style.color = modelUsageRange === 'today' ? 'var(--accent)' : '';
  _('modelUsageBtnAll').style.borderColor = modelUsageRange === 'all' ? 'var(--accent)' : '';
  _('modelUsageBtnAll').style.color = modelUsageRange === 'all' ? 'var(--accent)' : '';
  if (!lastModelUsage.length) {
    tbody.innerHTML = '<tr><td colspan="7" class="empty">' + t('暂无数据') + '</td></tr>';
    return;
  }
  const upNames = { cline: 'Cline', opencode: 'Zen' };
  tbody.innerHTML = lastModelUsage.map(m => {
    const today = modelUsageRange === 'today';
    const req = today ? m.todayRequests : m.requests;
    const input = today ? m.todayInputTokens : m.inputTokens;
    const output = today ? m.todayOutputTokens : m.outputTokens;
    const total = today ? m.todayTotalTokens : m.totalTokens;
    const ups = String(m.upstream || '').split('/').map(u => upNames[u] || u).join('/');
    return '<tr>' +
      '<td class="mono" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + esc(m.model) + '</td>' +
      '<td style="color:var(--text2)">' + esc(ups) + '</td>' +
      '<td style="text-align:right;font-variant-numeric:tabular-nums">' + formatNumber(req) + '</td>' +
      '<td style="text-align:right;font-variant-numeric:tabular-nums">' + formatTokenCount(input) + '</td>' +
      '<td style="text-align:right;font-variant-numeric:tabular-nums">' + formatTokenCount(output) + '</td>' +
      '<td style="text-align:right;font-variant-numeric:tabular-nums">' + formatTokenCount(m.cachedTokens) + '</td>' +
      '<td style="text-align:right;font-variant-numeric:tabular-nums;font-weight:600">' + formatTokenCount(total) + '</td>' +
      '</tr>';
  }).join('');
}

// ========== 账号管理 ==========
// ========== 账号管理：分页 + 勾选 ==========
let _accPage = 1;
const _accPageSize = 20;
const _checkedAccounts = new Set();
let _lastAccountList = []; // loadAccounts 缓存：全选本页/分页器复用，避免重复请求
let _accSearch = ''; // 邮箱搜索词（不区分大小写）
let _accSort = { key: null, dir: 1 }; // 列排序：key 见 _accSortValue，dir 1/-1

function _accSortValue(a, key) {
  switch (key) {
    case 'email': return (a.email || '').toLowerCase();
    case 'status': return a.status || '';
    case 'usage': return a.usageCount || 0;
    case 'total': return a.totalTokens || 0;
    case 'cached': return a.cachedTokens || 0;
    case 'lastUsed': return a.lastUsed ? new Date(a.lastUsed).getTime() : 0;
    case 'createdAt': return a.createdAt ? new Date(a.createdAt).getTime() : 0;
  }
  return null;
}

// 点击表头排序：同列切换升/降，新列默认升序
function onAccSort(key) {
  if (_accSort.key === key) _accSort.dir = -_accSort.dir;
  else _accSort = { key: key, dir: 1 };
  _accPage = 1;
  loadAccounts();
}

// 按搜索词过滤 + 列排序后的完整列表（分页/全选/计数都基于它）
function filteredAccountList() {
  let list = _lastAccountList;
  if (_accSearch) {
    const q = _accSearch.toLowerCase();
    list = list.filter(a => (a.email || '').toLowerCase().includes(q));
  }
  if (_accSort.key) {
    const key = _accSort.key, dir = _accSort.dir;
    list = list.slice().sort((a, b) => {
      const va = _accSortValue(a, key), vb = _accSortValue(b, key);
      if (va == null || vb == null) return 0;
      return (va < vb ? -1 : va > vb ? 1 : 0) * dir;
    });
  }
  return list;
}

function onAccSearch(v) {
  _accSearch = v.trim();
  _accPage = 1;
  loadAccounts();
}

async function loadAccounts() {
  try {
    const d = await api('GET', '/accounts');
    const list = d.data.accounts;
    _lastAccountList = list || [];
    const view = filteredAccountList(); // 搜索后的视图
    const tbody = _('accountTableBody');
    const cards = _('accountCards');
    const sc = _('accSearchCount');
    if (sc) sc.textContent = _accSearch ? (t('匹配') + ' ' + view.length + ' / ' + (_lastAccountList.length || 0)) : '';
    // 勾选状态跟随列表清理（已删除的账号从勾选中移除）
    if (list) {
      const ids = new Set(list.map(a => a.accountId));
      [..._checkedAccounts].forEach(id => { if (!ids.has(id)) _checkedAccounts.delete(id); });
    }
    if (!view || view.length === 0) {
      _accPage = 1;
      _('accountPager').style.display = 'none';
      updateSelCount();
      tbody.innerHTML = '<tr><td colspan="12" class="empty">' + (_accSearch ? (t('没有匹配的账号') + '："' + esc(_accSearch) + '"') : ('👋 还没有账号 — 前往 <a href="#" onclick="switchTab(\'import\')" style="color:var(--accent);cursor:pointer">' + t('导入账号') + '</a> ' + t('添加你的第一个 Cline 账号'))) + '</td></tr>';
      cards.innerHTML = '<div class="empty">' + (_accSearch ? (t('没有匹配的账号') + '："' + esc(_accSearch) + '"') : ('👋 还没有账号 — 前往 <a href="#" onclick="switchTab(\'import\')" style="color:var(--accent)">' + t('导入账号') + '</a> ' + t('添加你的第一个 Cline 账号'))) + '</div>';
      return;
    }
    // 分页：前端切片，页码越界时收敛
    const totalPages = Math.max(1, Math.ceil(view.length / _accPageSize));
    if (_accPage > totalPages) _accPage = totalPages;
    if (_accPage < 1) _accPage = 1;
    const start = (_accPage - 1) * _accPageSize;
    const pageList = view.slice(start, start + _accPageSize);
    updateSelCount();
    renderAccountPager(view.length);
    const sn = { active: t('活跃'), cooldown: t('冷却'), expired: t('已过期') };
    // 模型统计子行（仅 free 模型 + 模型级冷却状态）
    const modelStatsRow = a => {
      const stats = Object.values(a.modelStats || {}).sort((x, y) => y.totalTokens - x.totalTokens);
      const cools = a.modelCooldowns || {};
      const rows = stats.map(st => {
        const cd = cools[st.modelId];
        const cdBadge = cd
          ? '<span class="status cooldown status-cooldown" title="' + t('模型冷却中') + ' · ' + formatCooldown(cd) + '"><span class="cd-icon">⏳</span><span class="cd-time">' + formatCooldown(cd) + '</span></span>'
          : '';
        return '<tr style="background:var(--surface2)">' +
          '<td style="padding-left:32px" class="mono">' + esc(st.modelId) + ' <span class="model-tag free" style="font-size:10px;padding:1px 6px">free</span>' + '</td>' +
          '<td>' + cdBadge + '</td>' +
          '<td>' + formatNumber(st.usageCount) + '</td>' +
          '<td>' + formatTokenCount(st.promptTokens) + '</td>' +
          '<td>' + formatTokenCount(st.completionTokens) + '</td>' +
          '<td>' + formatTokenCount(st.totalTokens) + '</td>' +
          '<td>' + formatTokenCount(st.cachedTokens) + '</td>' +
          '</tr>';
      }).join('');
      const coolsWithoutStats = Object.keys(cools).filter(m => cools[m] && !(a.modelStats || {})[m]);
      const extraCools = coolsWithoutStats.map(m =>
        '<tr style="background:var(--surface2)">' +
          '<td style="padding-left:32px" class="mono">' + esc(m) + '</td>' +
          '<td><span class="status cooldown status-cooldown" title="' + t('模型冷却中') + ' · ' + formatCooldown(cools[m]) + '"><span class="cd-icon">⏳</span><span class="cd-time">' + formatCooldown(cools[m]) + '</span></span></td>' +
          '<td colspan="5"></td>' +
        '</tr>'
      ).join('');
      const totalCooling = Object.keys(cools).length;
      const title = '<tr style="background:var(--surface2)">' +
        '<td colspan="7" style="padding:8px 32px;color:var(--text2);font-size:12px;font-weight:600">' +
          t('按模型统计（仅免费模型）') + (totalCooling ? ' · <span style="color:var(--yellow)">⏳ ' + totalCooling + ' ' + t('模型冷却中') + '</span>' : '') +
        '</td></tr>';
      if (!rows && !extraCools) {
        return title + '<tr style="background:var(--surface2)"><td colspan="7" style="padding:6px 32px;color:var(--text3);font-size:12px">' + t('暂无数据') + '</td></tr>';
      }
      return title + rows + extraCools;
    };
    tbody.innerHTML = pageList.map((a, i) => {
      const seq = start + i + 1;
      const statusBadge = a.status === 'cooldown' && a.cooldownUntil
        ? '<span class="status cooldown status-cooldown" title="' + t('冷却 · 剩余 ') + formatCooldown(a.cooldownUntil) + '"><span class="cd-icon">⏳</span><span class="cd-time">' + formatCooldown(a.cooldownUntil) + '</span></span>'
        : '<span class="status ' + a.status + '"><span class="status-dot ' + a.status + '"></span>' + (sn[a.status] || a.status) + '</span>';
      // 始终显示模型统计展开按钮（无数据时子行提示暂无）
      const expander = '<button class="btn btn-sm btn-icon" onclick="toggleModelRow(\'' + a.accountId + '\', this)" title="' + t('展开') + '">▸</button>';
      const selBox = '<input type="checkbox" ' + (_checkedAccounts.has(a.accountId) ? 'checked' : '') + ' onchange="onSelAccount(\'' + a.accountId + '\', this.checked)">';
      return '<tr>' +
        '<td style="text-align:center">' + selBox + '</td>' +
        '<td class="mono" style="font-size:12px;color:var(--text3)">' + seq + '</td>' +
        '<td class="mono" style="font-size:12px" title="' + esc(a.email) + '">' + esc(a.email) + '</td>' +
        '<td>' + statusBadge + '</td>' +
        '<td>' + formatNumber(a.usageCount) + '</td>' +
        '<td>' + formatTokenCount(a.promptTokens) + '</td>' +
        '<td>' + formatTokenCount(a.completionTokens) + '</td>' +
        '<td>' + formatTokenCount(a.totalTokens) + '</td>' +
        '<td>' + formatTokenCount(a.cachedTokens) + '</td>' +
        '<td>' + fmtDateTime2(a.lastUsed) + '</td>' +
        '<td>' + fmtDateTime2(a.createdAt) + '</td>' +
        '<td style="white-space:nowrap">' + expander +
          '<button class="btn btn-sm" onclick="testAccount(\'' + a.accountId + '\',this)" title="测试">⚡</button> ' +
          '<button class="btn btn-sm" onclick="resetAccount(\'' + a.accountId + '\')" title="重置">↻</button> ' +
          '<button class="btn btn-sm btn-danger" onclick="deleteAccount(\'' + a.accountId + '\')" title="删除">✕</button>' +
        '</td></tr>' +
        '<tr id="modelRow-' + a.accountId + '" style="display:none"><td colspan="12" style="padding:0">' +
          '<table class="model-subtable" style="width:100%">' + modelStatsRow(a) + '</table></td></tr>';
    }).join('');
    cards.innerHTML = pageList.map(a => {
      const lu = a.lastUsed ? new Date(a.lastUsed).toLocaleString(LC()) : t('从未使用');
      const cardStatus = a.status === 'cooldown' && a.cooldownUntil
        ? '<span class="status cooldown status-cooldown" title="' + t('冷却 · 剩余 ') + formatCooldown(a.cooldownUntil) + '"><span class="cd-icon">⏳</span><span class="cd-time">' + formatCooldown(a.cooldownUntil) + '</span></span>'
        : '<span class="status ' + a.status + '"><span class="status-dot ' + a.status + '"></span>' + (sn[a.status] || a.status) + '</span>';
      // 卡片内的模型统计（免费模型 + 冷却状态）
      const stats = Object.values(a.modelStats || {}).sort((x, y) => y.totalTokens - x.totalTokens);
      const cools = a.modelCooldowns || {};
      const coolingCount = Object.keys(cools).length;
      let items = '';
      stats.forEach(st => {
        const cd = cools[st.modelId];
        items += '<div style="display:flex;justify-content:space-between;gap:8px;padding:5px 0;border-bottom:1px solid var(--border2);font-size:12px">' +
          '<span class="mono" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + esc(st.modelId) +
            (cd ? ' <span style="color:var(--yellow)">⏳' + formatCooldown(cd) + '</span>' : '') + '</span>' +
          '<span style="white-space:nowrap">' + formatTokenCount(st.totalTokens) + ' tok · ' + formatNumber(st.usageCount) + ' req</span></div>';
      });
      Object.keys(cools).filter(m => !(a.modelStats || {})[m]).forEach(m => {
        items += '<div style="display:flex;justify-content:space-between;gap:8px;padding:5px 0;border-bottom:1px solid var(--border2);font-size:12px">' +
          '<span class="mono" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + esc(m) + '</span>' +
          '<span style="color:var(--yellow);white-space:nowrap">⏳' + formatCooldown(cools[m]) + '</span></div>';
      });
      if (!items) items = '<div style="font-size:12px;color:var(--text3);padding:4px 0">' + t('暂无数据') + '</div>';
      const modelHtml = '<div style="margin:10px 0 0;padding:10px;border-radius:8px;background:var(--surface);border:1px solid var(--border2)">' +
        '<div style="font-size:11px;font-weight:600;color:var(--text2);margin-bottom:4px">' + t('按模型统计（仅免费模型）') +
          (coolingCount ? ' · <span style="color:var(--yellow)">⏳ ' + coolingCount + '</span>' : '') + '</div>' + items + '</div>';
      return '<article class="account-card">' +
        '<div class="account-card-header"><span class="account-email">' + esc(a.email) + '</span>' +
        cardStatus + '</div>' +
        '<div class="account-metrics">' +
          '<div class="account-metric"><span class="account-metric-label">' + t('请求') + '</span><span class="account-metric-value">' + formatNumber(a.usageCount) + '</span></div>' +
          '<div class="account-metric"><span class="account-metric-label">' + t('总 Token') + '</span><span class="account-metric-value">' + formatTokenCount(a.totalTokens) + '</span></div>' +
          '<div class="account-metric"><span class="account-metric-label">' + t('缓存') + '</span><span class="account-metric-value">' + formatTokenCount(a.cachedTokens) + '</span></div>' +
          '<div class="account-metric"><span class="account-metric-label">' + t('输入') + '</span><span class="account-metric-value">' + formatTokenCount(a.promptTokens) + '</span></div>' +
          '<div class="account-metric"><span class="account-metric-label">' + t('输出') + '</span><span class="account-metric-value">' + formatTokenCount(a.completionTokens) + '</span></div>' +
        '</div>' + modelHtml +
        '<div class="account-card-footer"><span>' + t('最后使用：') + lu + '</span><span class="account-card-actions">' +
          '<button class="btn btn-sm" onclick="testAccount(\'' + a.accountId + '\',this)" title="测试">⚡</button>' +
          '<button class="btn btn-sm" onclick="resetAccount(\'' + a.accountId + '\')" title="重置">↻</button>' +
          '<button class="btn btn-sm btn-danger" onclick="deleteAccount(\'' + a.accountId + '\')" title="删除">✕</button>' +
        '</span></div></article>';
    }).join('');
  } catch (e) { toast(t('加载账号失败: ') + e.message, 'error'); }
}

// 分页器：页码按钮 + 总数（total 为缓存列表长度）
function renderAccountPager(total) {
  const pager = _('accountPager');
  if (!pager) return;
  const totalPages = Math.max(1, Math.ceil(total / _accPageSize));
  if (totalPages <= 1 && total <= _accPageSize) { pager.style.display = 'none'; return; }
  pager.style.display = 'flex';
  pager.innerHTML =
    '<span style="font-size:12px;color:var(--text2)">' + t('共') + ' ' + total + ' ' + t('个账号 · 每页') + ' ' + _accPageSize + '</span>' +
    '<div style="display:flex;gap:6px;align-items:center">' +
      '<button class="btn btn-sm" ' + (_accPage <= 1 ? 'disabled' : '') + ' onclick="accGotoPage(' + (_accPage - 1) + ')">‹</button>' +
      '<span style="font-size:12px" class="mono">' + _accPage + ' / ' + totalPages + '</span>' +
      '<button class="btn btn-sm" ' + (_accPage >= totalPages ? 'disabled' : '') + ' onclick="accGotoPage(' + (_accPage + 1) + ')">›</button>' +
    '</div>';
}

function accGotoPage(p) {
  _accPage = p;
  loadAccounts();
}

// 勾选单个账号（跨页保持）
function onSelAccount(id, checked) {
  if (checked) _checkedAccounts.add(id);
  else _checkedAccounts.delete(id);
  updateSelCount();
}

// 全选/全不选当前页：优先用缓存列表；缓存为空（刚删除/导出后尚未刷新）时
// 退回当前渲染行上的勾选框（从 onchange="onSelAccount('id',...)" 解析账号 ID）。
function toggleAllAccountSel(box) {
  const list = filteredAccountList();
  const totalPages = Math.max(1, Math.ceil(list.length / _accPageSize));
  const p = Math.min(_accPage, totalPages);
  const chunk = list.slice((p - 1) * _accPageSize, p * _accPageSize);
  if (chunk.length) {
    chunk.forEach(a => {
      if (box.checked) _checkedAccounts.add(a.accountId);
      else _checkedAccounts.delete(a.accountId);
    });
    updateSelCount();
    loadAccounts();
    return;
  }
  const boxes = document.querySelectorAll('#accountTableBody input[type="checkbox"][onchange*="onSelAccount"]');
  if (!boxes.length) { loadAccounts(); return; }
  boxes.forEach(cb => {
    const m = (cb.getAttribute('onchange') || '').match(/onSelAccount\('([^']+)'/);
    if (!m) return;
    cb.checked = box.checked;
    if (box.checked) _checkedAccounts.add(m[1]);
    else _checkedAccounts.delete(m[1]);
  });
  updateSelCount();
}

// 勾选计数徽标：同时根据当前页是否全部勾选来同步“全选”框，避免勾上后被强制重置
function updateSelCount() {
  const n = _checkedAccounts.size;
  const sel = _('selCount'), del = _('delCount'), reset = _('resetCount'), all = _('accSelAll');
  if (sel) sel.textContent = n ? ' (' + n + ')' : '';
  if (del) del.textContent = n ? ' (' + n + ')' : '';
  if (reset) reset.textContent = n ? ' (' + n + ')' : '';
  if (all) {
    const list = filteredAccountList();
    const totalPages = Math.max(1, Math.ceil(list.length / _accPageSize));
    const p = Math.min(_accPage, totalPages);
    const chunk = list.slice((p - 1) * _accPageSize, p * _accPageSize);
    all.checked = chunk.length > 0 && chunk.every(a => _checkedAccounts.has(a.accountId));
  }
}

// 导出勾选的账号（POST /accounts/export {ids}，未勾选时提示）
async function exportSelectedAccounts() {
  const ids = [..._checkedAccounts];
  if (!ids.length) { toast(t('请先勾选要导出的账号'), 'info'); return; }
  try {
    const res = await fetch(API + '/accounts/export', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ids: ids })
    });
    if (!res.ok) throw new Error('HTTP ' + res.status);
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'cline-accounts-export.json';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
    toast(t('已导出 ') + ids.length + t(' 个账号'), 'success');
  } catch (e) { toast(t('导出失败: ') + e.message, 'error'); }
}

// 删除勾选的账号（逐个调用现有删除接口，带确认）
// 重置勾选的账号（逐个调用现有重置接口，清除冷却/失败计数）
async function resetSelectedAccounts(btn) {
  const ids = [..._checkedAccounts];
  if (!ids.length) { toast(t('请先勾选要重置的账号'), 'info'); return; }
  if (!confirm(t('确认重置选中的 ') + ids.length + t(' 个账号？（清除冷却与失败计数）'))) return;
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('重置中...'); }
  let ok = 0, fail = 0;
  for (const id of ids) {
    try { await api('POST', '/accounts/reset', { accountId: id }); ok++; }
    catch (e) { fail++; }
  }
  _checkedAccounts.clear();
  if (btn) { btn.disabled = false; btn.innerHTML = orig; }
  if (fail) toast(t('已重置 ') + ok + t(' 个，失败 ') + fail + t(' 个'), 'error');
  else toast(t('已重置 ') + ok + t(' 个账号'), 'success');
  await loadAccounts();
}

async function deleteSelectedAccounts(btn) {
  const ids = [..._checkedAccounts];
  if (!ids.length) { toast(t('请先勾选要删除的账号'), 'info'); return; }
  if (!confirm(t('确认删除选中的 ') + ids.length + t(' 个账号？此操作不可恢复'))) return;
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('删除中...'); }
  let ok = 0, fail = 0;
  for (const id of ids) {
    try { await api('POST', '/accounts/delete', { accountId: id }); ok++; }
    catch (e) { fail++; }
  }
  _checkedAccounts.clear();
  if (btn) { btn.disabled = false; btn.innerHTML = orig; }
  if (fail) toast(t('已删除 ') + ok + t(' 个，失败 ') + fail + t(' 个失败'), 'error');
  else toast(t('已删除 ') + ok + t(' 个账号'), 'success');
  await loadAccounts();
  await loadStats();
}

// 表格时间列：日期一行、时间灰色小字一行，固定不折行
function fmtDateTime2(v) {
  if (!v) return '<span style="color:var(--text3)">-</span>';
  const d = new Date(v);
  return '<div style="white-space:nowrap">' + d.toLocaleDateString(LC()) + '</div>' +
    '<div style="white-space:nowrap;font-size:10px;color:var(--text3)">' + d.toLocaleTimeString(LC()) + '</div>';
}

// 展开/收起账号的模型统计子行（表格视图）
function toggleModelRow(id, btn) {
  const row = _('modelRow-' + id);
  if (!row) return;
  const hidden = row.style.display === 'none';
  row.style.display = hidden ? '' : 'none';
  btn.innerHTML = hidden ? '▾' : '▸';
  btn.title = hidden ? t('收起') : t('展开');
}

async function testAccount(id, btn) {
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>'; }
  try {
    const d = await api('POST', '/accounts/test', { accountId: id });
    const r = (d.data.results || [])[0];
    if (r && r.ok) {
      const tok = r.inputTokens || r.outputTokens ? t(' · 输入 ') + formatTokenCount(r.inputTokens) + t(' · 输出 ') + formatTokenCount(r.outputTokens) : '';
      toast(t('测试成功：') + esc(r.email) + ' · ' + formatDuration(r.durationMs) + tok, 'success');
    } else {
      toast(t('测试失败：') + esc(r ? r.email : '?') + ' · ' + (r ? r.error : t('未知错误')), 'error');
    }
    loadAccounts(); loadStats();
  } catch (e) { toast(t('测试失败: ') + e.message, 'error'); }
  if (btn) { btn.disabled = false; btn.innerHTML = orig; }
}

async function testAllAccounts(btn) {
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('测试中...'); }
  toast(t('正在测试全部账号，请稍候...'), 'info');
  try {
    const d = await api('POST', '/accounts/test', {});
    const results = d.data.results || [];
    const ok = results.filter(r => r.ok).length;
    const fail = results.length - ok;
    if (fail === 0) {
      toast(t('全部测试通过：') + ok + '/' + results.length + t(' 个账号正常'), 'success');
    } else {
      const failed = results.filter(r => !r.ok).map(r => esc(r.email) + '(' + r.error + ')').join('，');
      toast(t('测试完成：') + ok + t(' 成功 / ') + fail + t(' 失败 · ') + failed, 'error');
    }
    loadAccounts(); loadStats();
  } catch (e) { toast(t('测试失败: ') + e.message, 'error'); }
  if (btn) { btn.disabled = false; btn.innerHTML = orig; }
}

async function deleteAccount(id) {
  if (!confirm(t('确定删除此账号？'))) return;
  try {
    await api('POST', '/accounts/delete', { accountId: id });
    toast(t('账号已删除'), 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(t('删除失败: ') + e.message, 'error'); }
}

async function resetAccount(id) {
  if (!confirm(t('确定重置此账号？将恢复为活跃状态并刷新 Token，保留历史统计。'))) return;
  try {
    await api('POST', '/accounts/reset', { accountId: id });
    toast(t('账号已重置'), 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(t('重置失败: ') + e.message, 'error'); }
}

async function deleteAllAccounts() {
  if (!confirm(t('⚠️ 确定删除所有账号？不可撤销！'))) return;
  try {
    await api('POST', '/accounts/delete-all', {});
    toast(t('全部账号已删除'), 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(t('删除失败: ') + e.message, 'error'); }
}

async function refreshAllTokens() {
  try {
    await api('POST', '/accounts/refresh-all', {});
    toast(t('全部 Token 已刷新'), 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(t('刷新失败: ') + e.message, 'error'); }
}

// ========== OAuth 登录 ==========
async function startOAuth() {
  const btn = _('oauthBtn');
  btn.disabled = true;
  btn.innerHTML = '<span class="loading"></span> ' + t('启动中...');
  _('oauthProgress').style.display = 'block';
  _('oauthResult').style.display = 'none';
  _('oauthStatus').textContent = t('正在连接 WorkOS...');
  try {
    const d = await api('POST', '/oauth/start');
    const s = d.data;
    _('oauthStatus').textContent = t('请在浏览器中打开链接并输入代码');
    const u = _('oauthUrl');
    u.textContent = s.verificationUri;
    u.href = '#';
    u.onclick = async function(e) { e.preventDefault(); await openExternal(s.verificationUri); };
    _('oauthUserCode').textContent = s.userCode;
    const poll = setInterval(async () => {
      try {
        const r = await api('GET', '/oauth/status?sessionId=' + s.sessionId);
        if (r.data.done) {
          clearInterval(poll);
          btn.disabled = false;
          btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M12 5l7 7-7 7"/></svg>' + t('开始 OAuth 登录');
          if (r.data.success) {
            _('oauthProgress').style.display = 'none';
            _('oauthResult').innerHTML = '<div style="color:var(--green);font-weight:600">✓ ' + t('账号添加成功: ')+ esc(r.data.email) + '</div>';
            _('oauthResult').style.display = 'block';
            loadAccounts(); loadStats();
            toast(t('账号添加成功！'), 'success');
          } else {
            _('oauthStatus').textContent = t('失败: ') + (r.data.error || t('未知错误'));
            toast(t('OAuth 失败'), 'error');
          }
        }
      } catch(e) {}
    }, 2000);
  } catch (e) {
    btn.disabled = false;
    btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M12 5l7 7-7 7"/></svg>' + t('开始 OAuth 登录');
    _('oauthStatus').textContent = t('错误: ') + e.message;
    toast(t('OAuth 失败: ') + e.message, 'error');
  }
}

// ========== Token 导入 ==========
async function addByToken() {
  const token = _('tokenInput').value.trim();
  if (!token) { toast(t('请输入 refreshToken'), 'error'); return; }
  const email = _('tokenEmail').value.trim();
  try {
    const d = await api('POST', '/accounts/add', { refreshToken: token, email: email || undefined });
    toast(t('账号添加成功: ') + (d.data.email || ''), 'success');
    _('tokenInput').value = '';
    _('tokenEmail').value = '';
    loadAccounts(); loadStats();
  } catch (e) { toast(t('添加失败: ') + e.message, 'error'); }
}

// ========== 导出账号 ==========
async function exportAccounts() {
  try {
    const res = await fetch(API + '/accounts/export');
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'cline-accounts-export.json';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
    toast(t('账号已导出'), 'success');
  } catch (e) { toast(t('导出失败: ') + e.message, 'error'); }
}

// ========== 批量导入 ==========
// 解析导入数据：支持导出格式 {tokens:[...]}、JSON 数组 [...]、单行一个 token 的纯文本
// 解析后按 refreshToken 去重，避免同一账号被重复导入
function parseImportData(text) {
  let parsed;
  try {
    parsed = JSON.parse(text);
  } catch {
    // 纯文本：每行一个 refreshToken
    parsed = text.split('\n').filter(t => t.trim()).map(t => ({ refreshToken: t.trim() }));
  }
  let list;
  // 导出格式 {tokens:[...], exportedAt:...}
  if (parsed && !Array.isArray(parsed) && Array.isArray(parsed.tokens)) {
    list = parsed.tokens;
  // JSON 数组
  } else if (Array.isArray(parsed)) {
    list = parsed;
  // 单个对象
  } else {
    list = [parsed];
  }
  const seen = new Set();
  return list
    .map(item => (item && typeof item === 'object') ? item : { refreshToken: String(item || '').trim() })
    .filter(item => {
      const tok = (item.refreshToken || '').trim();
      if (!tok || seen.has(tok)) return false;
      seen.add(tok);
      return true;
    });
}

async function batchImport() {
  const raw = _('batchInput').value.trim();
  if (!raw) { toast(t('请输入账号数据'), 'error'); return; }
  const tokens = parseImportData(raw);
  try {
    const d = await api('POST', '/batch-import', { tokens });
    toast(d.message || t('导入完成'), 'success');
    _('batchInput').value = '';
    loadAccounts(); loadStats();
  } catch (e) { toast(t('导入失败: ') + e.message, 'error'); }
}

async function handleFileImport(event) {
  const file = event.target.files[0];
  if (!file) return;
  const text = await file.text();
  const tokens = parseImportData(text);
  try {
    const d = await api('POST', '/batch-import', { tokens });
    const imported = (d.data && typeof d.data.imported === 'number') ? d.data.imported : tokens.length;
    toast(d.message || t('导入了 ') + imported + t(' 个账号'), 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(t('导入失败: ') + e.message, 'error'); }
  event.target.value = '';
}

// ========== API 密钥管理 ==========
async function loadKeys() {
  try {
    const d = await api('GET', '/keys');
    const keys = d.data.keys;
    const el = _('keysList');
    if (!keys || keys.length === 0) {
      el.innerHTML = '<div class="empty-state"><div class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg></div>' + t('暂无 API 密钥') + '</div>';
      return;
    }
    el.innerHTML = keys.map(k =>
      '<div class="flex" style="margin-bottom:8px">' +
        '<span class="key-display" style="flex:1" onclick="copyText(\'' + k + '\')" title="点击复制">' + esc(k) + '</span>' +
        '<button class="btn btn-sm btn-danger" onclick="deleteKey(\'' + k + '\')">✕</button>' +
      '</div>'
    ).join('');
  } catch (e) { _('keysList').innerHTML = '<div class="empty">' + t('加载失败') + '</div>'; }
}

async function generateKey() {
  try {
    const d = await api('POST', '/keys/generate');
    const key = d.data.key;
    _('keyGenResult').innerHTML =
      '<div style="background:var(--green-soft);border:1px solid var(--green);border-radius:var(--radius-sm);padding:14px">' +
        '<div style="color:var(--green);font-weight:600;margin-bottom:8px">✓ ' + t('新密钥已生成（点击复制）') + '</div>' +
        '<div class="key-display" onclick="copyText(\'' + key + '\')">' + esc(key) + '</div>' +
      '</div>';
    loadKeys();
    toast(t('密钥已生成'), 'success');
    setTimeout(() => _('keyGenResult').innerHTML = '', 8000);
  } catch (e) { toast(t('生成失败: ') + e.message, 'error'); }
}

async function deleteKey(key) {
  if (!confirm(t('确定删除此密钥？'))) return;
  try {
    await api('POST', '/keys/delete', { key });
    toast(t('密钥已删除'), 'success');
    loadKeys();
  } catch (e) { toast(t('删除失败: ') + e.message, 'error'); }
}

async function deleteAllKeys() {
  if (!confirm(t('确定删除所有 API 密钥？'))) return;
  try {
    const d = await api('GET', '/keys');
    const keys = d.data.keys || [];
    for (const k of keys) await api('POST', '/keys/delete', { key: k });
    toast(t('全部密钥已删除'), 'success');
    loadKeys();
  } catch (e) { toast(t('删除失败: ') + e.message, 'error'); }
}

function copyText(t) {
  navigator.clipboard.writeText(t).then(() => toast(t('已复制到剪贴板'), 'success')).catch(() => {
    const ta = document.createElement('textarea');
    ta.value = t; document.body.appendChild(ta); ta.select(); document.execCommand('copy'); document.body.removeChild(ta);
    toast(t('已复制到剪贴板'), 'success');
  });
}

// ========== 配置管理 ==========
async function updateConfig() {
  const strategy = _('settingStrategy').value;
  const defaultModel = _('settingDefModel').value;
  const chainField = _('settingModelChain');
  const modelChain = chainField.value.split(',').map(s => s.trim()).filter(Boolean);
  const onlyFree = _('settingOnlyFree') ? _('settingOnlyFree').value === 'true' : undefined;
  try {
    await api('POST', '/config/update', { strategy, defaultModel, modelChain, onlyFree });
    delete chainField.dataset.dirty;
    toast(t('配置已更新'), 'success');
  } catch (e) { toast(t('更新失败: ') + e.message, 'error'); }
}

// 保存监听地址：保存后程序自动重启监听（立即生效）
async function saveListenHost() {
  const host = _('settingListenHost').value;
  if (!host) return;
  try {
    const d = await api('POST', '/config/update', { host });
    const safe = ['127.0.0.1', 'localhost', '::1', '0.0.0.0'].indexOf(host) !== -1;
    if (safe) {
      toast(t('已保存，正在重启监听...'), 'success');
      setTimeout(() => location.reload(), 1500);
    } else {
      toast(t('监听已切换，请通过 ') + (d.data.address || host) + t(' 访问管理后台'), 'success');
    }
    await loadConfig();
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
}

// 保存/清除管理后台密码（留空 = 清除）
async function savePassword() {
  const pwd = _('settingPassword').value;
  if (pwd && pwd.length < 4) { toast(t('密码至少 4 位'), 'error'); return; }
  try {
    await api('POST', '/password', { password: pwd });
    _('settingPassword').value = '';
    toast(pwd ? t('密码已设置，后台需要重新登录') : t('已清除密码'), 'success');
    await loadConfig();
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
}

function addHeaderRow() {
  const tbody = _('headersTableBody');
  const tr = document.createElement('tr');
  tr.innerHTML =
    '<td><input type="text" class="header-key" placeholder="Header-Name" style="font-size:12px;font-family:ui-monospace,monospace"></td>' +
    '<td><input type="text" class="header-val" placeholder="value" style="font-size:12px;font-family:ui-monospace,monospace"></td>' +
    '<td><button class="btn btn-sm btn-danger" onclick="this.closest(\'tr\').remove()">✕</button></td>';
  tbody.appendChild(tr);
}

// 批量编辑：把表格转成 textarea，一行一个 Key: Value
async function bulkEditHeaders() {
  const tbody = _('headersTableBody');
  const rows = tbody.querySelectorAll('tr');
  const lines = [];
  rows.forEach(tr => {
    const k = tr.querySelector('.header-key'), v = tr.querySelector('.header-val');
    if (k && k.value.trim()) lines.push(k.value.trim() + ': ' + (v ? v.value.trim() : ''));
  });
  const box = document.createElement('div');
  box.innerHTML =
    '<div class="field" style="margin-top:10px"><label>批量编辑（每行 Key: Value）</label>' +
    '<textarea id="headersBulk" rows="10" style="width:100%;font-family:ui-monospace,monospace;font-size:12px;border:1px solid var(--border2);border-radius:8px;padding:8px;background:var(--surface);color:var(--text)">' + esc(lines.join('\n')) + '</textarea></div>' +
    '<div class="form-actions" style="margin-top:8px"><button class="btn btn-sm btn-primary" onclick="applyBulkHeaders()">应用并保存</button>' +
    '<button class="btn btn-sm" onclick="loadConfig()">取消</button></div>';
  tbody.parentElement.parentElement.querySelector('.form-actions').style.display = 'none';
  tbody.parentElement.parentElement.appendChild(box);
}

async function applyBulkHeaders() {
  const text = _('headersBulk').value;
  const headers = {};
  text.split('\n').forEach(line => {
    const idx = line.indexOf(':');
    if (idx <= 0) return;
    const k = line.slice(0, idx).trim();
    const v = line.slice(idx + 1).trim();
    if (k) headers[k] = v;
  });
  try {
    await api('POST', '/config/update', { headers });
    toast(t('请求头已保存') + ' (' + Object.keys(headers).length + ')', 'success');
    loadConfig();
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
}

// ========== opencode 请求头 ==========
function parseHeaderText(text) {
  const out = {};
  text.split('\n').forEach(line => {
    const idx = line.indexOf(':');
    if (idx <= 0) return;
    const k = line.slice(0, idx).trim();
    const v = line.slice(idx + 1).trim();
    if (k) out[k] = v;
  });
  return out;
}

async function loadZenHeaders() {
  try {
    const d = await api('GET', '/opencode/config');
    const c = d.data || {};
    const hdrs = c.zenHeaders || {};
    _('zenHeadersBulk').value = Object.entries(hdrs).map(([k, v]) => k + ': ' + v).join('\n');
  } catch (e) { /* ignore */ }
}

async function saveZenHeaders() {
  try {
    await api('POST', '/opencode/config/update', { zenHeaders: parseHeaderText(_('zenHeadersBulk').value) });
    toast(t('opencode 请求头已保存'), 'success');
    _('zenHeaderSaveResult').innerHTML = '<div style="color:var(--green);font-size:13px">✓ ' + t('已保存') + '</div>';
    setTimeout(() => _('zenHeaderSaveResult').innerHTML = '', 4000);
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
}

// ========== 自定义 Provider ==========
let _editingProviderId = '';

async function loadProviders() {
  try {
    const d = await api('GET', '/providers');
    const list = d.data.providers || [];
    const box = _('providersList');
    if (!list.length) { box.innerHTML = '<div class="empty" style="padding:10px;color:var(--text3)">' + t('暂无自定义 Provider') + '</div>'; return; }
    box.innerHTML = '<table><thead><tr><th>名称</th><th>Base URL</th><th>模型</th><th>优先级</th><th>状态</th><th style="width:220px"></th></tr></thead><tbody>' +
      list.map(p =>
        '<tr>' +
          '<td>' + esc(p.name) + (p.free ? ' <span style="color:var(--green);font-size:11px">free</span>' : '') + '</td>' +
          '<td class="mono" style="font-size:11px;max-width:220px;overflow:hidden;text-overflow:ellipsis">' + esc(p.baseURL) + '</td>' +
          '<td style="font-size:11px;max-width:200px;overflow:hidden;text-overflow:ellipsis">' + esc((p.modelIds || []).join(', ')) + '</td>' +
          '<td>' + p.priority + '</td>' +
          '<td>' + (p.enabled ? '<span style="color:var(--green)">✓</span>' : '<span style="color:var(--text3)">停用</span>') + '</td>' +
          '<td><button class="btn btn-sm" onclick="editProvider(\'' + p.id + '\')">编辑</button> ' +
              '<button class="btn btn-sm" onclick=\'testProviderById("' + p.id + '")\'>测试</button> ' +
              '<button class="btn btn-sm" onclick=\'toggleProvider("' + p.id + '", ' + !p.enabled + ')\'>' + (p.enabled ? t('停用') : t('启用')) + '</button> ' +
              '<button class="btn btn-sm btn-danger" onclick=\'deleteProvider("' + p.id + '")\'>✕</button></td>' +
        '</tr>'
      ).join('') + '</tbody></table>';
  } catch (e) { /* ignore */ }
}

// toggleProvider 快速切换 provider 启停（不打开编辑表单）：拉取最新配置，
// 翻转 enabled 后原样回存，其余字段不动。停用即时退出路由与模型同步。
async function toggleProvider(id, enable) {
  try {
    const d = await api('GET', '/providers');
    const p = (d.data.providers || []).find(x => x.id === id);
    if (!p) { toast(t('Provider 未找到'), 'error'); return; }
    p.enabled = !!enable;
    p.modelIds = p.modelIds || [];
    await api('POST', '/providers/save', p);
    toast(t(enable ? '已启用' : '已停用') + ': ' + p.name, 'success');
    loadProviders();
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
}

function showProviderEditor(p) {
  _editingProviderId = p ? p.id : '';
  _('provName').value = p ? p.name : '';
  _('provBaseURL').value = p ? p.baseURL : '';
  _('provKey').value = p ? (p.apiKey || '') : '';
  _('provPriority').value = p ? p.priority : 10;
  _('provTimeout').value = p ? (p.timeoutSec || 300) : 300;
  _('provRpmLimit').value = p ? (p.rpmLimit || 0) : 0;
  _('provDailyLimit').value = p ? (p.dailyRequestLimit || 0) : 0;
  _('provModels').value = p ? (p.modelIds || []).join(', ') : '';
  _('provHeaders').value = p ? Object.entries(p.headers || {}).map(([k, v]) => k + ': ' + v).join('\n') : '';
  _('provEnabled').value = p ? String(p.enabled !== false) : 'true';
  _('provFree').value = p ? String(!!p.free) : 'true';
  _('providerEditor').style.display = '';
}

function hideProviderEditor() { _('providerEditor').style.display = 'none'; }

async function saveProvider() {
  const payload = {
    id: _editingProviderId,
    name: _('provName').value.trim(),
    baseURL: _('provBaseURL').value.trim(),
    apiKey: _('provKey').value.trim(),
    priority: parseInt(_('provPriority').value, 10) || 10,
    timeoutSec: parseInt(_('provTimeout').value, 10) || 300,
    rpmLimit: parseInt(_('provRpmLimit').value, 10) || 0,
    dailyRequestLimit: parseInt(_('provDailyLimit').value, 10) || 0,
    modelIds: _('provModels').value.split(',').map(s => s.trim()).filter(Boolean),
    headers: parseHeaderText(_('provHeaders').value),
    enabled: _('provEnabled').value === 'true',
    free: _('provFree').value === 'true',
  };
  try {
    await api('POST', '/providers/save', payload);
    toast(t('Provider 已保存'), 'success');
    hideProviderEditor();
    loadProviders();
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
}

function editProvider(id) {
  api('GET', '/providers').then(d => {
    const p = (d.data.providers || []).find(x => x.id === id);
    if (p) showProviderEditor(p);
  });
}

async function deleteProvider(id) {
  if (!confirm(t('确定删除该 Provider？'))) return;
  try {
    await api('POST', '/providers/delete', { id });
    toast(t('已删除'), 'success');
    loadProviders();
  } catch (e) { toast(t('删除失败: ') + e.message, 'error'); }
}

async function testProviderById(id) {
  _('provTestResult').textContent = '...';
  try {
    const d = await api('POST', '/providers/test', { id });
    const r = d.data;
    _('provTestResult').innerHTML = r.ok
      ? '<span style="color:var(--green)">✓ ' + r.durationMs + 'ms</span>'
      : '<span style="color:var(--red)">✗ ' + esc(r.error || 'failed') + '</span>';
  } catch (e) { _('provTestResult').innerHTML = '<span style="color:var(--red)">✗</span>'; }
}

async function testProvider() {
  // 先保存再测试（测试走已保存配置）
  await saveProvider();
  setTimeout(async () => {
    try {
      const d = await api('GET', '/providers');
      const list = d.data.providers || [];
      const mine = list.filter(p => p.name === _('provName').value.trim());
      if (mine.length) testProviderById(mine[0].id);
    } catch (e) {}
  }, 400);
}

async function loadProviderPresets() {
  const box = _('providerPresets');
  if (box.style.display !== 'none') { box.style.display = 'none'; return; }
  try {
    const d = await api('GET', '/providers/presets');
    const presets = d.data.presets || [];
    box.innerHTML = '<div style="font-weight:600;margin-bottom:8px">' + t('点击预设快速填充') + ':</div>' +
      '<div style="display:flex;flex-wrap:wrap;gap:8px">' + presets.map(p =>
        '<button class="btn btn-sm" title="' + esc(p.notes) + '" onclick=\'applyPreset("' + p.key + '")\'>' +
        esc(p.name) + (p.freeTier ? ' 🆓' : '') + '</button>'
      ).join('') + '</div>' +
      '<div style="font-size:11px;color:var(--text3);margin-top:8px">🆓 = 提供免费额度（需自行注册 API Key）</div>';
    box.style.display = '';
    box.dataset.presets = JSON.stringify(presets);
  } catch (e) {}
}

function applyPreset(key) {
  const presets = JSON.parse(_('providerPresets').dataset.presets || '[]');
  const p = presets.find(x => x.key === key);
  if (!p) return;
  showProviderEditor(null);
  _('provName').value = p.name;
  _('provBaseURL').value = p.baseURL;
  _('provHeaders').value = Object.entries(p.headers || {}).map(([k, v]) => k + ': ' + v).join('\n');
  _('providerPresets').style.display = 'none';
  toast(t('已填充预设，请补 API Key 和模型 ID'), 'info');
}

async function saveHeaders() {
  const tbody = _('headersTableBody');
  const rows = tbody.querySelectorAll('tr');
  const headers = {};
  let hasEmpty = false;
  rows.forEach(tr => {
    const keyInput = tr.querySelector('.header-key');
    const valInput = tr.querySelector('.header-val');
    if (keyInput && valInput) {
      const k = keyInput.value.trim();
      const v = valInput.value.trim();
      if (k) { headers[k] = v; }
      else if (v) { hasEmpty = true; }
    }
  });
  if (hasEmpty) { toast(t('存在有值无键的行，已忽略'), 'info'); }
  try {
    const d = await api('POST', '/config/update', { headers });
    toast(t('请求头已保存'), 'success');
    _('headerSaveResult').innerHTML =
      '<div style="color:var(--green);font-size:13px">✓ ' + t('已保存 ')+ Object.keys(d.data.headers).length + t(' 个请求头') + '</div>';
    setTimeout(() => _('headerSaveResult').innerHTML = '', 5000);
    loadConfig();
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
}

// ========== 模型组合（Combo） ==========
var _comboList = [];
var _comboCatalog = [];
var _comboReserved = [];
var _comboEntries = [];
var _editingComboId = '';

// 组合模型来源显示名：provider → 真实 Provider 名；remote → Cline；zen/seed → opencode；其余 → 自定义
function comboSourceName(m) {
  if (m.source === 'provider') return m.provider || t('自定义');
  if (m.source === 'remote') return 'Cline';
  if (m.source === 'zen' || m.source === 'seed') return 'opencode';
  return t('自定义');
}

// loadCombos 拉取组合列表 + 模型目录 + 保留别名；可安全地重复调用
async function loadCombos() {
  try {
    const d = await api('GET', '/combos');
    _comboList = d.data.combos || [];
    _comboCatalog = d.data.modelCatalog || [];
    _comboReserved = d.data.reservedAliases || [];
    renderCombosList();
    // 编辑面板打开时目录可能已更新，重绘条目下拉（不丢失当前编辑内容）
    const ed = _('comboEditor');
    if (ed && ed.style.display !== 'none') renderComboEntries();
  } catch (e) {
    const box = _('combosList');
    if (box) box.innerHTML = '<div class="empty">' + t('加载失败') + ': ' + esc(e.message) + '</div>';
  }
}

function comboCatalogEntry(id) {
  return _comboCatalog.find(m => m.id === id) || null;
}

// 组合列表渲染：id / 名称 / 分类 / 条目数 / 状态 / 操作
function renderCombosList() {
  const box = _('combosList');
  if (!box) return;
  if (!_comboList.length) {
    box.innerHTML = '<div class="empty">' + t('暂无模型组合') + '</div>';
    return;
  }
  box.innerHTML = '<div class="combos-list"><table><thead><tr>' +
    '<th>' + t('组合 ID') + '</th><th>' + t('名称') + '</th><th>' + t('分类') + '</th>' +
    '<th>' + t('条目') + '</th><th>' + t('状态') + '</th><th style="width:280px"></th>' +
    '</tr></thead><tbody>' +
    _comboList.map(c => {
      const entries = c.entries || [];
      const enabledEntries = entries.filter(e => !e.disabled).length;
      const badge = c.enabled
        ? '<span class="log-status ok">' + t('已启用') + '</span>'
        : '<span class="log-status fail">' + t('已停用') + '</span>';
      const fb = c.emergencyFallback
        ? ' <span class="model-source-badge provider" title="' + t('允许跳出组合回退') + '">' + t('紧急回退') + '</span>'
        : '';
      return '<tr>' +
        '<td class="mono" style="font-size:12px">' + esc(c.id) + '</td>' +
        '<td>' + esc(c.name || '') + fb + '</td>' +
        '<td>' + (c.category ? esc(c.category) : '<span style="color:var(--text3)">-</span>') + '</td>' +
        '<td class="row-count">' + enabledEntries + ' / ' + entries.length + '</td>' +
        '<td>' + badge + '</td>' +
        '<td><button class="btn btn-sm" onclick="editCombo(\'' + escAttr(c.id) + '\')">' + t('编辑') + '</button> ' +
            '<button class="btn btn-sm" onclick="cloneCombo(\'' + escAttr(c.id) + '\')">' + t('克隆') + '</button> ' +
            '<button class="btn btn-sm" onclick="toggleCombo(\'' + escAttr(c.id) + '\', ' + !c.enabled + ')">' + t(c.enabled ? '停用' : '启用') + '</button> ' +
            '<button class="btn btn-sm btn-danger" onclick="deleteCombo(\'' + escAttr(c.id) + '\')">✕</button></td>' +
      '</tr>';
    }).join('') +
    '</tbody></table></div>';
}

function showComboEditor(c) {
  _editingComboId = c ? c.id : '';
  _('comboId').value = c ? c.id : '';
  _('comboId').disabled = !!c; // 编辑既有组合时 id 不可改（主键）
  _('comboName').value = c ? (c.name || '') : '';
  _('comboDesc').value = c ? (c.description || '') : '';
  _('comboCategory').value = c ? (c.category || '') : '';
  _('comboEnabled').value = c ? String(c.enabled !== false) : 'true';
  _('comboEmergency').checked = c ? !!c.emergencyFallback : false;
  _comboEntries = c ? (c.entries || []).map(e => ({ model: e.model || '', disabled: !!e.disabled, source: e.source || '', provider: e.provider || '' })) : [];
  _('comboSaveResult').textContent = '';
  renderComboEntries();
  _('comboEditor').style.display = '';
  try { _('comboEditor').scrollIntoView({ behavior: 'smooth', block: 'nearest' }); } catch (e) {}
}

function hideComboEditor() {
  _('comboEditor').style.display = 'none';
  _editingComboId = '';
  _comboEntries = [];
}

// 组合下拉/条目标签：id · <来源名> · 免费/付费（不重复显示 Provider 名）
function comboModelLabel(m) {
  return m.id + '  ·  ' + comboSourceName(m) + '  ·  ' + (m.cost === 'free' ? t('免费') : t('付费'));
}

// 组合下拉分组：按来源归组（Provider 用真实名）
function comboOptgroupKey(m) {
  if (m.source === 'provider') return 'prov:' + (m.provider || 'Provider');
  if (m.source === 'remote') return 'remote';
  if (m.source === 'zen' || m.source === 'seed') return 'zen';
  return 'custom';
}
function comboGroupSortKey(a, b) {
  const ord = k => k === 'remote' ? 0 : (k === 'zen' ? 1 : (k.indexOf('prov:') === 0 ? 2 : 3));
  const oa = ord(a), ob = ord(b);
  if (oa !== ob) return oa - ob;
  return a.localeCompare(b);
}

// 构建模型下拉：按来源分 <optgroup>；排除管理页已停用的模型；当前条目即使停用/缺失也始终保留为选中项
function buildComboModelOptions(current, chosenMap) {
  let html = '<option value=""' + (!current ? ' selected' : '') + '>' + t('— 选择模型 —') + '</option>';
  const cur = current ? comboCatalogEntry(current) : null;
  // 保留当前引用：停用标记为「模型已停用」，不在目录中标记为「目录中未找到」，绝不静默丢弃
  if (current && (!cur || cur.disabled)) {
    const label = cur
      ? esc(comboModelLabel(cur)) + '  ·  ' + t('模型已停用')
      : esc(current) + '  ·  ' + t('目录中未找到');
    html += '<option value="' + escAttr(current) + '" selected>' + label + '</option>';
  }
  const groups = new Map(); // groupKey -> { label, items[] }
  const seen = {};
  for (const m of _comboCatalog) {
    if (m.disabled) continue;         // 已停用模型不可选
    if (seen[m.id]) continue;
    seen[m.id] = true;
    const gk = comboOptgroupKey(m);
    if (!groups.has(gk)) groups.set(gk, { label: comboSourceName(m), items: [] });
    groups.get(gk).items.push(m);
  }
  const gkeys = Array.from(groups.keys()).sort(comboGroupSortKey);
  for (const gk of gkeys) {
    const g = groups.get(gk);
    html += '<optgroup label="' + escAttr(g.label) + '">';
    g.items.sort((a, b) => a.id.localeCompare(b.id));
    for (const m of g.items) {
      const used = chosenMap[m.id] && m.id !== current;
      html += '<option value="' + escAttr(m.id) + '"' + (m.id === current ? ' selected' : '') + (used ? ' disabled' : '') + '>' +
        esc(comboModelLabel(m)) + (used ? '  ·  ' + t('已使用') : '') + '</option>';
    }
    html += '</optgroup>';
  }
  return html;
}

// 选中模型的小徽章：来源 / 提供商 / 计费 / 停用 / 下架
function comboEntryBadges(id) {
  if (!id) return '<span style="font-size:11px;color:var(--text3)">' + t('未选择模型') + '</span>';
  const m = comboCatalogEntry(id);
  if (!m) return '<span class="delisted-badge" style="border-color:var(--yellow);color:var(--yellow)">' + t('目录中未找到') + '</span>';
  let b = '';
  let srcKey = 'custom';
  if (m.source === 'provider') srcKey = 'provider';
  else if (m.source === 'remote') srcKey = 'remote';
  else if (m.source === 'zen' || m.source === 'seed') srcKey = 'zen';
  b += '<span class="model-source-badge ' + srcKey + '">' + esc(comboSourceName(m)) + '</span>';
  if (m.provider && m.source !== 'provider') b += '<span class="model-source-badge">' + esc(m.provider) + '</span>';
  b += m.cost === 'free'
    ? '<span class="model-tag free" style="padding:1px 7px;font-size:10px;margin:0">' + t('免费') + '</span>'
    : '<span class="model-tag pass" style="padding:1px 7px;font-size:10px;margin:0">' + t('付费') + '</span>';
  if (m.disabled) b += '<span class="delisted-badge" style="border-color:var(--yellow);color:var(--yellow)">' + t('模型已停用') + '</span>';
  if (m.delisted) b += '<span class="delisted-badge">' + t('已下架') + '</span>';
  return b;
}

// 目录行 → 上游 pin 元组（与后端 source 取值一致：cline / zen / provider）
function comboCatalogPin(m) {
  if (m.source === 'provider') return { source: 'provider', provider: m.provider || '' };
  if (m.source === 'zen' || m.source === 'seed') return { source: 'zen', provider: '' };
  return { source: 'cline', provider: '' }; // remote 或 ""
}

function comboPinKey(pin) {
  return (pin && pin.source ? pin.source : '') + '|' + ((pin && pin.provider) ? pin.provider : '');
}

function comboPinLabel(pin) {
  if (!pin || !pin.source) return '';
  if (pin.source === 'zen') return 'opencode';
  if (pin.source === 'cline') return 'Cline';
  return pin.provider || t('自定义');
}

// 同一模型 ID 在目录中可用的去重上游列表（按 pin 元组去重；<2 个时不需选择器）
function comboEntryUpstreams(modelId) {
  if (!modelId) return [];
  const map = new Map();
  for (const m of _comboCatalog) {
    if (m.id !== modelId) continue;
    const pin = comboCatalogPin(m);
    const k = comboPinKey(pin);
    if (!map.has(k)) map.set(k, pin);
  }
  const pins = Array.from(map.values());
  const ord = p => p.source === 'cline' ? 0 : (p.source === 'zen' ? 1 : 2);
  pins.sort((a, b) => {
    const oa = ord(a), ob = ord(b);
    return oa !== ob ? oa - ob : comboPinLabel(a).localeCompare(comboPinLabel(b));
  });
  return pins;
}

// 仅当该模型有多个上游时渲染「上游」选择器；value: "" 自动 | cline | zen | provider|<Name>
function comboSourceSelectHtml(i, e) {
  const ups = comboEntryUpstreams(e.model);
  if (ups.length < 2) return '';
  const curKey = comboPinKey({ source: e.source, provider: e.provider });
  let opts = '<option value=""' + (!e.source ? ' selected' : '') + '>' + t('自动') + '</option>';
  for (const p of ups) {
    const val = p.source === 'provider' ? ('provider|' + p.provider) : p.source;
    opts += '<option value="' + escAttr(val) + '"' + (comboPinKey(p) === curKey ? ' selected' : '') + '>' + esc(comboPinLabel(p)) + '</option>';
  }
  return '<span class="combo-entry-source"><select title="' + t('上游') + '" onchange="onComboEntrySourceChange(' + i + ', this.value)">' + opts + '</select></span>';
}

// 条目编辑器渲染 + 优先级预览
function renderComboEntries() {
  const box = _('comboEntries');
  if (!box) return;
  if (!_comboEntries.length) {
    box.innerHTML = '<div class="combo-empty-entries">' + t('尚未添加条目，点击下方「添加条目」开始。') + '</div>';
    renderComboPreview();
    return;
  }
  const chosen = {};
  _comboEntries.forEach(e => { if (e.model) chosen[e.model] = true; });
  box.innerHTML = _comboEntries.map((e, i) => {
    const opts = buildComboModelOptions(e.model, chosen);
    const dis = e.disabled ? ' disabled-entry' : '';
    return '<div class="combo-entry' + dis + '">' +
      '<span class="combo-entry-idx">' + (i + 1) + '</span>' +
      '<span class="combo-entry-select"><select onchange="onComboEntryModelChange(' + i + ', this.value)">' + opts + '</select></span>' +
      comboSourceSelectHtml(i, e) +
      '<span class="combo-entry-badges">' + comboEntryBadges(e.model) + '</span>' +
      '<span class="combo-entry-actions">' +
        '<label class="switch" title="' + t('启用/停用该条目') + '"><input type="checkbox"' + (e.disabled ? '' : ' checked') + ' onchange="onComboEntryDisabled(' + i + ', !this.checked)"><span class="slider"></span></label>' +
        '<button class="btn btn-sm" onclick="moveComboEntry(' + i + ', -1)"' + (i === 0 ? ' disabled' : '') + ' title="' + t('上移') + '">↑</button>' +
        '<button class="btn btn-sm" onclick="moveComboEntry(' + i + ', 1)"' + (i === _comboEntries.length - 1 ? ' disabled' : '') + ' title="' + t('下移') + '">↓</button>' +
        '<button class="btn btn-sm btn-danger" onclick="removeComboEntry(' + i + ')" title="' + t('移除') + '">✕</button>' +
      '</span>' +
    '</div>';
  }).join('');
  renderComboPreview();
}

function renderComboPreview() {
  const box = _('comboPreview');
  if (!box) return;
  const enabled = _comboEntries.filter(e => !e.disabled && e.model);
  if (!enabled.length) {
    box.innerHTML = '<span style="color:var(--text3)">' + t('（暂无启用的条目）') + '</span>';
    return;
  }
  box.innerHTML = enabled.map((e, i) =>
    '<span class="step">' + (i + 1) + '. ' + esc(e.model) + '</span>'
  ).join('<span class="arrow">→</span>');
}

function addComboEntry() {
  _comboEntries.push({ model: '', disabled: false, source: '', provider: '' });
  renderComboEntries();
}

function removeComboEntry(i) {
  if (i < 0 || i >= _comboEntries.length) return;
  _comboEntries.splice(i, 1);
  renderComboEntries();
}

function onComboEntryModelChange(i, val) {
  const e = _comboEntries[i];
  if (!e) return;
  e.model = val;
  // 新模型若不再提供当前 pin 的上游，则清空为「自动」，避免残留无效钉选
  if (e.source) {
    const key = comboPinKey({ source: e.source, provider: e.provider });
    const stillValid = comboEntryUpstreams(val).some(p => comboPinKey(p) === key);
    if (!stillValid) { e.source = ''; e.provider = ''; }
  }
  renderComboEntries();
}

// 上游选择变化："" 清空；cline/zen 设定来源；provider|<Name> 设定 Provider 来源
function onComboEntrySourceChange(i, value) {
  const e = _comboEntries[i];
  if (!e) return;
  if (value === 'cline' || value === 'zen') {
    e.source = value; e.provider = '';
  } else if (value && value.indexOf('provider|') === 0) {
    e.source = 'provider'; e.provider = value.slice(9);
  } else {
    e.source = ''; e.provider = '';
  }
  renderComboPreview();
}

function onComboEntryDisabled(i, disabled) {
  if (_comboEntries[i]) _comboEntries[i].disabled = !!disabled;
  renderComboEntries();
}

function moveComboEntry(i, dir) {
  const j = i + dir;
  if (j < 0 || j >= _comboEntries.length) return;
  const tmp = _comboEntries[i];
  _comboEntries[i] = _comboEntries[j];
  _comboEntries[j] = tmp;
  renderComboEntries();
}

async function saveCombo() {
  const id = _('comboId').value.trim();
  const name = _('comboName').value.trim();
  const res = _('comboSaveResult');
  res.textContent = '';
  if (!id) { toast(t('请填写组合 ID'), 'error'); return; }
  if (!name) { toast(t('请填写名称'), 'error'); return; }
  if (!_editingComboId && _comboReserved.indexOf(id) !== -1) { toast(t('该 ID 为保留别名，请更换'), 'error'); return; }
  const entries = _comboEntries.filter(e => e.model).map(e => {
    const out = { model: e.model, disabled: !!e.disabled };
    if (e.source) {
      out.source = e.source;
      if (e.source === 'provider' && e.provider) out.provider = e.provider;
    }
    return out;
  });
  const seen = {};
  for (const e of entries) {
    if (seen[e.model]) { toast(t('条目中存在重复模型: ') + e.model, 'error'); return; }
    seen[e.model] = true;
  }
  const payload = {
    id: id,
    name: name,
    description: _('comboDesc').value.trim(),
    category: _('comboCategory').value.trim(),
    enabled: _('comboEnabled').value === 'true',
    emergencyFallback: _('comboEmergency').checked,
    entries: entries,
  };
  try {
    await api('POST', '/combos/save', payload);
    toast(t('组合已保存'), 'success');
    hideComboEditor();
    await loadCombos();
  } catch (e) {
    res.innerHTML = '<span style="color:var(--red)">✗ ' + esc(e.message) + '</span>';
    toast(t('保存失败: ') + e.message, 'error');
  }
}

function editCombo(id) {
  const c = _comboList.find(x => x.id === id);
  if (c) showComboEditor(c);
}

async function toggleCombo(id, enabled) {
  try {
    await api('POST', '/combos/toggle', { id: id, enabled: !!enabled });
    toast(t(enabled ? '已启用' : '已停用') + ': ' + id, 'success');
    await loadCombos();
  } catch (e) { toast(t('保存失败: ') + e.message, 'error'); }
}

async function deleteCombo(id) {
  if (!confirm(t('确定删除该组合？'))) return;
  try {
    await api('POST', '/combos/delete', { id: id });
    toast(t('已删除'), 'success');
    if (_editingComboId === id) hideComboEditor();
    await loadCombos();
  } catch (e) { toast(t('删除失败: ') + e.message, 'error'); }
}

async function cloneCombo(id) {
  try {
    await api('POST', '/combos/clone', { id: id });
    toast(t('已克隆'), 'success');
    await loadCombos();
  } catch (e) { toast(t('克隆失败: ') + e.message, 'error'); }
}

// ========== 模型列表 ==========
let _cachedModels = [];
let _modelSyncSeen = false;
let _modelGroupOpen = {}; // 分组折叠状态（localStorage 持久化）
const _modelFilters = { search: '', provider: '', cost: '', status: '' };
const MODEL_GROUP_OPEN_KEY = 'cline_admin_model_groups_open';
try {
  const _mgo = JSON.parse(localStorage.getItem(MODEL_GROUP_OPEN_KEY) || '{}');
  if (_mgo && typeof _mgo === 'object') _modelGroupOpen = _mgo;
} catch (e) {}

// ===== 两级分组：一级 = 真实来源（Cline / Zen / Provider / 自定义），二级 = 厂商 =====
// mergedSourceKey：合并行的主来源，优先级 自定义 > Provider > zen > Cline
function mergedSourceKey(m) {
  if (m._customAny) return 'custom';
  if (m._sources.indexOf('provider') !== -1) return 'provider';
  if (m._sources.indexOf('zen') !== -1) return 'zen';
  return 'remote';
}

// 一级分组 key：src:cline / src:zen / src:prov:<name> / src:custom / src:other
function sourceGroupKey(m) {
  const src = mergedSourceKey(m);
  if (src === 'provider') return 'src:prov:' + ((m._provNames && m._provNames[0]) || m.provider || 'Provider');
  if (src === 'zen') return 'src:zen';
  if (src === 'custom') return 'src:custom';
  if (src === 'remote') return 'src:cline';
  return 'src:other';
}

function sourceGroupLabel(key) {
  if (key === 'src:cline') return t('Cline');
  if (key === 'src:zen') return t('Zen (opencode)');
  if (key.indexOf('src:prov:') === 0) return key.slice(9);
  if (key === 'src:custom') return t('用户自定义');
  return t('其他');
}

function sourceSortKey(a, b) {
  const ord = k => k === 'src:cline' ? 0 : (k === 'src:zen' ? 1 : (k.indexOf('src:prov:') === 0 ? 2 : (k === 'src:custom' ? 3 : 4)));
  const oa = ord(a), ob = ord(b);
  if (oa !== ob) return oa - ob;
  return a.localeCompare(b);
}

// 二级（厂商）key：模型 ID 第一个 "/" 前的前缀；无前缀回退 provider 字段
function vendorKey(m) {
  const idx = (m.id || '').indexOf('/');
  if (idx > 0) return m.id.slice(0, idx);
  return m.provider || '__other';
}

function vendorLabel(key) { return key === '__other' ? t('其他') : key; }

function vendorSortKey(a, b) {
  if (a === '__other') return 1;
  if (b === '__other') return -1;
  return a.localeCompare(b);
}

function modelSource(m) {
  if (m.custom) return 'custom';
  if (m.source === 'provider') return 'provider';
  if (m.source === 'zen' || m.provider === 'opencode') return 'zen';
  return 'remote';
}
const MODEL_SOURCE_LABEL = { zen: 'opencode', remote: 'Cline', provider: 'Provider' };

// mergeModelsById 同一模型 ID 可能同时被 Cline 与 opencode 同步（池内多条目）：
// 合并为一行展示 —— 来源/计费取并集；delisted 区分「任一下架/全部下架」
// （任一来源仍在官方列表就不算整体下架）；启停开关按 ID 作用到所有来源条目。
function mergeModelsById(models) {
  const map = new Map();
  for (const m of models) {
    const prev = map.get(m.id);
    if (!prev) {
      map.set(m.id, {
        id: m.id, provider: m.provider || '', context: m.context || 0, output: m.output || 0,
        _sources: [modelSource(m)],
        _costs: [m.cost || 'free'],
        _provNames: (modelSource(m) === 'provider' && m.provider) ? [m.provider] : [],
        _delistedAny: !!m.delisted,
        _delistedAll: !!m.delisted,
        _customAny: !!m.custom,
        _disabled: !!m.disabled,
      });
      continue;
    }
    const src = modelSource(m);
    if (prev._sources.indexOf(src) === -1) prev._sources.push(src);
    if (prev._costs.indexOf(m.cost || 'free') === -1) prev._costs.push(m.cost || 'free');
    // 同一模型 ID 可被多个自定义 Provider 同步进来（同名不同源）：
    // 全部来源名字都亮出来，肉眼即可分辨同一模型的两个上游
    if (src === 'provider' && m.provider && prev._provNames.indexOf(m.provider) === -1) prev._provNames.push(m.provider);
    prev._delistedAny = prev._delistedAny || !!m.delisted;
    prev._delistedAll = prev._delistedAll && !!m.delisted;
    prev._customAny = prev._customAny || !!m.custom;
    prev._disabled = prev._disabled || !!m.disabled;
    if (!prev.provider && m.provider) prev.provider = m.provider;
    if ((m.context || 0) > prev.context) prev.context = m.context || 0;
    if ((m.output || 0) > prev.output) prev.output = m.output || 0;
  }
  return Array.from(map.values());
}

function onModelFilterChange(key, value) {
  _modelFilters[key] = value;
  renderModelList();
}

function modelMatchesFilters(m) {
  if (_modelFilters.search && m.id.toLowerCase().indexOf(_modelFilters.search.toLowerCase()) === -1) return false;
  if (_modelFilters.provider && sourceGroupKey(m) !== _modelFilters.provider) return false;
  if (_modelFilters.cost && m._costs.indexOf(_modelFilters.cost) === -1) return false;
  if (_modelFilters.status === 'disabled' && !m._disabled) return false;
  if (_modelFilters.status === 'enabled' && m._disabled) return false;
  return true;
}

// 工具栏「来源」筛选：按一级来源分组 key 生成选项
function buildProviderOptions(merged) {
  const sel = _('modelFilterProvider');
  if (!sel) return;
  const keys = Array.from(new Set(merged.map(sourceGroupKey))).sort(sourceSortKey);
  const cur = _modelFilters.provider || sel.value;
  sel.innerHTML = '<option value="">' + t('全部来源') + '</option>' +
    keys.map(k => '<option value="' + escAttr(k) + '"' + (k === cur ? ' selected' : '') + '>' + esc(sourceGroupLabel(k)) + '</option>').join('');
}

function renderModelStats(merged) {
  const bar = _('modelStatsBar');
  if (!bar) return;
  if (!merged.length) { bar.innerHTML = ''; return; }
  const disabled = merged.filter(m => m._disabled).length;
  const delisted = merged.filter(m => m._delistedAll).length;
  bar.innerHTML =
    '<span>' + t('共') + ' <b>' + merged.length + '</b> ' + t('个模型') + '</span>' +
    '<span><span class="dot g"></span>' + t('已启用') + ' <b>' + (merged.length - disabled) + '</b></span>' +
    '<span><span class="dot y"></span>' + t('已停用') + ' <b>' + disabled + '</b></span>' +
    (delisted ? '<span><span class="dot r"></span>' + t('已下架') + ' <b>' + delisted + '</b></span>' : '');
}

function renderModelRow(m) {
  let tags = '';
  const hasFree = m._costs.indexOf('free') !== -1;
  const hasPass = m._costs.indexOf('pass') !== -1;
  if (hasFree && hasPass) {
    tags += '<span class="model-tag free">' + t('免费') + '</span><span class="model-tag pass">' + t('付费') + '</span>';
  } else if (hasFree) {
    tags += '<span class="model-tag free">' + t('免费') + '</span>';
  } else {
    tags += '<span class="model-tag pass">' + t('付费') + '</span>';
  }
  if (m._delistedAll) {
    tags += '<span class="delisted-badge" title="' + t('已从上游官方列表移除，实测可能仍可用；同步只标记不删除，上游报模型不存在时自动清理') + '">' + t('已下架') + '</span>';
  }
  for (const s of m._sources) {
    if (s === 'custom') tags += '<span class="model-source-badge custom">' + t('自定义') + '</span>';
    else if (s === 'provider') {
      // 别名标识： Provider 来源的 badge 显示真实名称（多源同名模型全部列出）
      const n = (m._provNames && m._provNames.length) ? m._provNames.join(' · ') : (m.provider || MODEL_SOURCE_LABEL[s]);
      tags += '<span class="model-source-badge provider" title="' + esc(m.id) + '">' + esc(n) + '</span>';
    }
    else tags += '<span class="model-source-badge ' + s + '">' + MODEL_SOURCE_LABEL[s] + '</span>';
  }
  let meta = '';
  if (m.context) meta += 'ctx ' + fmtTokens(m.context);
  if (m.output) meta += (meta ? ' · ' : '') + 'out ' + fmtTokens(m.output);
  // 所有模型都开放上下文/最大输出编辑：Cline 模型不在已知硬限制表时，
  // 手动设置的 Output 会作为 modelMaxOutputLimit 的封顶值生效
  let actions = '<button class="btn btn-sm" style="padding:2px 6px" onclick="editModelMeta(\'' + esc(m.id) + '\',' + m.context + ',' + m.output + ')" title="' + t('上下文窗口 / 最大输出 (token)，点击⚙修改') + '">⚙</button>';
  if (m._customAny || m._delistedAny) {
    actions += '<button class="btn btn-sm btn-danger" style="padding:2px 6px" onclick="deleteModel(\'' + esc(m.id) + '\')" title="' + (m._delistedAny ? t('移除（已下架模型支持手动移除）') : t('删除')) + '">✕</button>';
  }
  return '<div class="model-row' + (m._disabled ? ' disabled-row' : '') + '">' +
    '<label class="switch" title="' + (m._disabled ? t('点击启用该模型') : t('点击停用该模型')) + '"><input type="checkbox"' + (m._disabled ? '' : ' checked') + ' onchange="toggleModel(\'' + esc(m.id) + '\', !this.checked)"><span class="slider"></span></label>' +
    '<div class="model-row-main"><span class="model-row-id mono">' + esc(m.id) + '</span>' + tags + '</div>' +
    '<div class="model-row-meta" title="' + t('上下文窗口 / 最大输出 (token)，点击⚙修改') + '">' + meta + '</div>' +
    '<div class="model-row-actions">' + actions + '</div>' +
  '</div>';
}

// 模型管理列表渲染：两级分组（来源 → 厂商）+ 工具栏筛选；两级都可折叠、可批量启停
function renderModelList() {
  const listEl = _('modelsList');
  if (!listEl) return;
  const merged = mergeModelsById(_cachedModels);
  renderModelStats(merged);
  buildProviderOptions(merged);
  if (!merged.length) {
    listEl.innerHTML = '<div class="empty">' + t('暂无模型') + '</div>';
    return;
  }
  const filtered = merged.filter(modelMatchesFilters);
  if (!filtered.length) {
    listEl.innerHTML = '<div class="empty">' + t('无匹配模型') + '</div>';
    return;
  }
  // 一级：来源；二级：厂商
  const sources = new Map(); // sourceKey -> Map(vendorKey -> rows[])
  for (const m of filtered) {
    const sk = sourceGroupKey(m);
    const vk = vendorKey(m);
    if (!sources.has(sk)) sources.set(sk, new Map());
    const vmap = sources.get(sk);
    if (!vmap.has(vk)) vmap.set(vk, []);
    vmap.get(vk).push(m);
  }
  const groupActions = () =>
    '<span class="model-group-actions" onclick="event.stopPropagation()">' +
      '<button class="btn" onclick="toggleModelsBatchEl(this, false)">' + t('全部启用') + '</button>' +
      '<button class="btn" onclick="toggleModelsBatchEl(this, true)">' + t('全部停用') + '</button>' +
    '</span>';
  const groupHead = (key, label, count, open, src) =>
    '<div class="model-group-head' + (src ? ' model-group-src-head' : '') + (open ? ' expanded' : '') + '" data-key="' + escAttr(key) + '" onclick="toggleModelGroupEl(this)" title="' + t('点击展开/折叠') + '">' +
      '<span class="model-group-caret">▸</span>' +
      '<span class="model-group-label">' + esc(label) + '</span>' +
      '<span class="model-group-count">' + count + '</span>' +
      groupActions() +
    '</div>';
  listEl.innerHTML = Array.from(sources.keys()).sort(sourceSortKey).map(sk => {
    const vmap = sources.get(sk);
    let total = 0;
    vmap.forEach(rows => { total += rows.length; });
    const sOpen = _modelGroupOpen[sk] !== false;
    const vendors = Array.from(vmap.keys()).sort(vendorSortKey);
    const inner = vendors.map(vk => {
      const rows = vmap.get(vk).slice().sort((a, b) => a.id.localeCompare(b.id));
      const vPath = sk + '/' + vk;
      const vOpen = _modelGroupOpen[vPath] !== false;
      return '<div class="model-group model-group-vendor">' +
        groupHead(vPath, vendorLabel(vk), rows.length, vOpen, false) +
        '<div class="model-group-body" style="display:' + (vOpen ? 'block' : 'none') + '">' + rows.map(renderModelRow).join('') + '</div>' +
      '</div>';
    }).join('');
    return '<div class="model-group model-group-src">' +
      groupHead(sk, sourceGroupLabel(sk), total, sOpen, true) +
      '<div class="model-group-body" style="display:' + (sOpen ? 'block' : 'none') + '">' + inner + '</div>' +
    '</div>';
  }).join('');
}

// 折叠/展开分组（两级通用）：直接读自身 data-key，避免把 key 拼进 onclick 字符串
function toggleModelGroupEl(head) {
  if (!head) return;
  const key = head.dataset.key || '';
  const open = !head.classList.contains('expanded');
  head.classList.toggle('expanded', open);
  const body = head.nextElementSibling; // 直接相邻的 .model-group-body
  if (body && body.classList.contains('model-group-body')) body.style.display = open ? 'block' : 'none';
  _modelGroupOpen[key] = open;
  try { localStorage.setItem(MODEL_GROUP_OPEN_KEY, JSON.stringify(_modelGroupOpen)); } catch (e) {}
}

// 启用/停用模型：按 ID 提交（池内同 ID 的多来源条目一并切换），后端在
// 停用当前默认模型时会自动清空默认；完成后刷新列表与默认模型下拉。
async function toggleModel(id, disabled) {
  try {
    await api('POST', '/models/toggle', { ids: [id], disabled: disabled });
    toast((disabled ? t('已停用') : t('已启用')) + ': ' + id, 'success');
  } catch (e) {
    toast(t('保存失败: ') + e.message, 'error');
  }
  await loadModels();
  if (_('settingDefModel')) loadConfig();
}

// 将分组 key 解析为合并行集合：src:xxx 匹配整个来源；src:xxx/vendor 匹配单一厂商
function mergedRowsForGroupKey(key) {
  const merged = mergeModelsById(_cachedModels);
  if (!key || key.indexOf('src:') !== 0) return merged.filter(m => vendorKey(m) === key);
  const slash = key.indexOf('/');
  if (slash === -1) return merged.filter(m => sourceGroupKey(m) === key);
  const srcKey = key.slice(0, slash);
  const vKey = key.slice(slash + 1);
  return merged.filter(m => sourceGroupKey(m) === srcKey && vendorKey(m) === vKey);
}

// 分组按钮入口：从按钮所在分组头读取 key（两级通用）
function toggleModelsBatchEl(btn, disabled) {
  const head = btn && btn.closest ? btn.closest('.model-group-head') : null;
  if (!head) return;
  toggleModelsBatch(head.dataset.key || '', disabled);
}

async function toggleModelsBatch(groupKey, disabled) {
  const ids = mergedRowsForGroupKey(groupKey).map(m => m.id);
  if (!ids.length) return;
  try {
    await api('POST', '/models/toggle', { ids: ids, disabled: disabled });
    toast((disabled ? t('已停用') : t('已启用')) + ' ' + ids.length + ' ' + t('个模型'), 'success');
  } catch (e) {
    toast(t('保存失败: ') + e.message, 'error');
  }
  await loadModels();
  if (_('settingDefModel')) loadConfig();
}

// 免费导向的批量开关：一次作用于全部合并行（不受工具栏筛选影响）
async function bulkModelToggle(mode) {
  const merged = mergeModelsById(_cachedModels);
  let rows, disabled, btn, confirmMsg;
  if (mode === 'free') {
    rows = merged.filter(m => m._costs.indexOf('free') !== -1);
    disabled = false;
    btn = _('bulkEnableFreeBtn');
    confirmMsg = t('确定启用全部免费模型？');
  } else {
    // 停用付费：仅纯付费模型（free+pass 混合的保持启用，优先保免费）
    rows = merged.filter(m => m._costs.indexOf('pass') !== -1 && m._costs.indexOf('free') === -1);
    disabled = true;
    btn = _('bulkDisablePaidBtn');
    confirmMsg = t('确定停用全部付费模型？');
  }
  const ids = rows.map(m => m.id);
  if (!ids.length) { toast(t('没有符合条件的模型'), 'info'); return; }
  if (!window.confirm(confirmMsg + ' (' + ids.length + ')')) return;
  if (btn) btn.disabled = true;
  try {
    await api('POST', '/models/toggle', { ids: ids, disabled: disabled });
    toast((disabled ? t('已停用') : t('已启用')) + ' ' + ids.length + ' ' + t('个模型'), 'success');
  } catch (e) {
    toast(t('保存失败: ') + e.message, 'error');
  }
  if (btn) btn.disabled = false;
  await loadModels();
  if (_('settingDefModel')) loadConfig();
}

function toggleAddModelRow() {
  const row = _('modelAddRow');
  row.style.display = (row.style.display === 'none') ? 'flex' : 'none';
  if (row.style.display === 'flex') _('newModelId').focus();
}

function fmtTokens(n) {
  if (n >= 1000000) return (n / 1000000).toFixed(n % 1000000 ? 1 : 0) + 'M';
  if (n >= 1000) return Math.round(n / 1000) + 'K';
  return String(n);
}

// 修改模型上下文/最大输出：压缩阈值按此计算，设置后 zen 同步保留该值
async function editModelMeta(id, ctx, out) {
  const c = prompt(t('上下文窗口 (token)') + ' - ' + id, ctx || 1048576);
  if (c === null) return;
  const o = prompt(t('最大输出 (token)'), out || 32768);
  if (o === null) return;
  try {
    await api('POST', '/models/context', { id: id, context: parseInt(c, 10) || 0, output: parseInt(o, 10) || 0 });
    toast(t('已保存') + ': ' + id + ' ctx=' + fmtTokens(parseInt(c, 10) || 0), 'success');
    await loadModels();
  } catch (e) { toast(t('保存失败') + ': ' + e.message, 'error'); }
}

// 模型分组渲染已迁移至 renderModelList（按提供商分组 + 筛选 + 批量启停）

async function loadModels() {
  try {
    const d = await api('GET', '/models');
    const models = d.data.models || [];
    _cachedModels = models;
    renderModelList();
    const ls = d.data.lastSync || {};
    if (_('modelSyncTime')) {
      _('modelSyncTime').textContent = ls.syncedAt ? new Date(ls.syncedAt).toLocaleString(LC()) : t('从未同步');
    }
    // 启动自动同步的变更弹窗（仅提示一次）
    if (!_modelSyncSeen && ls.syncedAt && ls.changed) {
      _modelSyncSeen = true;
      showModelSyncModal(ls);
    }
  } catch (e) { _('modelsList').textContent = t('加载失败'); }
}

async function syncModels() {
  const btn = _('syncModelsBtn');
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('同步中...'); }
  try {
    const d = await api('POST', '/models/sync');
    const res = d.data || {};
    await loadModels();
    if (res.changed) {
      showModelSyncModal(res);
      toast(t('模型列表已更新'), 'success');
    } else {
      toast(t('模型无变化'), 'info');
    }
    if (_('settingDefModel')) await loadConfig();
  } catch (e) {
    toast(t('模型同步失败') + ': ' + (e.message || ''), 'error');
  }
  if (btn) { btn.disabled = false; btn.innerHTML = orig; }
}

async function syncOcModels() {
  const btn = _('syncOcModelsBtn');
  const orig = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span> ' + t('同步中...'); }
  try {
    const d = await api('POST', '/opencode/models/sync');
    const res = d.data || {};
    await loadModels();
    if (res.changed) {
      showModelSyncModal(res);
      toast(t('opencode 模型列表已更新'), 'success');
    } else {
      toast(t('模型无变化'), 'info');
    }
  } catch (e) {
    toast(t('模型同步失败') + ': ' + (e.message || ''), 'error');
  }
  if (btn) { btn.disabled = false; btn.innerHTML = orig; }
}

// ========== opencode 免费模型配置 ==========
async function loadOcConfig() {
  try {
    const d = await api('GET', '/opencode/config');
    const c = d.data;
    _('ocEnabled').value = String(!!c.enabled);
    if (c.key) _('ocKey').value = c.key;
    if (c.baseURL) _('ocBaseURL').value = c.baseURL;
    _('ocMaxConcurrency').value = c.maxConcurrency;
    _('ocRetries').value = c.retries;
    _('ocFailover').value = String(!!c.failover);
    _('ocFailoverCount').value = c.failoverCount;
    _('ocFailoverMinutes').value = c.failoverMinutes;
    const cp = c.compaction || {};
    _('ocCompactAuto').value = String(!!cp.auto);
    _('ocCompactBuffer').value = cp.buffer != null ? cp.buffer : 20000;
    _('ocCompactKeepTokens').value = cp.keepTokens != null ? cp.keepTokens : 8000;
    _('ocCompactMaxSummary').value = cp.maxSummary != null ? cp.maxSummary : 4096;
    _('ocProxyStrategy').value = c.proxyStrategy || 'round_robin';
    _('ocProxies').value = (c.proxies || []).join('\n');
    // 运行状态与出口冷却
    const rt = c.runtime || {};
    let status;
    if (!c.enabled) status = '<span style="color:var(--text3)">⏸ ' + t('已停用') + '</span>';
    else if (rt.failoverActive) status = '<span style="color:var(--red)">🔴 ' + t('故障转移中（opencode 暂不可用，请求走 Cline 池）') + '</span>';
    else status = '<span style="color:var(--green)">🟢 ' + t('正常') + '</span>' +
      '<span style="margin-left:8px">' + t('已同步模型') + ': ' + (c.syncedModels || 0) + '</span>';
    _('ocRuntimeStatus').innerHTML = status;
    const cds = c.proxyCooldowns || {};
    const entries = Object.entries(cds);
    _('ocProxyCooldowns').textContent = entries.length
      ? entries.map(([p, until]) => maskProxyForDisplay(p) + ' → ' + until).join('; ')
      : t('无冷却');
  } catch (e) { /* ignore */ }
}

function maskProxyForDisplay(p) {
  return p.replace(/\/\/([^@/]+)@/, '//***@');
}

async function saveOcConfig() {
  const numOr = (id, def) => { const v = parseInt(_(id).value, 10); return isNaN(v) ? def : v; };
  const payload = {
    enabled: _('ocEnabled').value === 'true',
    key: _('ocKey').value.trim(),
    baseURL: _('ocBaseURL').value.trim(),
    maxConcurrency: numOr('ocMaxConcurrency', 8),
    retries: numOr('ocRetries', 3),
    failover: _('ocFailover').value === 'true',
    failoverCount: numOr('ocFailoverCount', 3),
    failoverMinutes: numOr('ocFailoverMinutes', 5),
    proxyStrategy: _('ocProxyStrategy').value,
    proxies: _('ocProxies').value.split('\n').map(s => s.trim()).filter(Boolean),
    compaction: {
      auto: _('ocCompactAuto').value === 'true',
      buffer: numOr('ocCompactBuffer', 20000),
      keepTokens: numOr('ocCompactKeepTokens', 8000),
      maxSummary: numOr('ocCompactMaxSummary', 4096),
    },
  };
  try {
    await api('POST', '/opencode/config/update', payload);
    _('ocSaveResult').innerHTML = '<span style="color:var(--green)">✓ ' + t('opencode 配置已保存') + '</span>';
    setTimeout(() => _('ocSaveResult').innerHTML = '', 5000);
    await loadOcConfig();
  } catch (e) {
    toast(t('保存失败: ') + (e.message || ''), 'error');
  }
}

async function loadClineProxyConfig() {
  try {
    const d = await api('GET', '/cline-proxy/config');
    const c = d.data;
    _('clineProxyStrategy').value = c.proxyStrategy || 'round_robin';
    _('clineProxies').value = (c.proxies || []).join('\n');
  } catch (e) { /* ignore */ }
}

async function saveClineProxyConfig() {
  const payload = {
    proxyStrategy: _('clineProxyStrategy').value,
    proxies: _('clineProxies').value.split('\n').map(s => s.trim()).filter(Boolean),
  };
  try {
    await api('POST', '/cline-proxy/config/update', payload);
    _('clineProxySaveResult').innerHTML = '<span style="color:var(--green)">✓ ' + t('Cline 代理配置已保存') + '</span>';
    setTimeout(() => _('clineProxySaveResult').innerHTML = '', 5000);
    await loadClineProxyConfig();
  } catch (e) {
    toast(t('保存失败: ') + (e.message || ''), 'error');
  }
}

function showModelSyncModal(res) {
  const ov = _('modelSyncOverlay');
  if (!ov) return;
  const box = _('modelSyncModal');
  const add = (res.added || []).map(m => '<div style="padding:3px 0">+ <span class="mono">' + esc(m) + '</span></div>').join('');
  const rem = (res.removed || []).map(m => '<div style="padding:3px 0;color:var(--text3)">- <span class="mono">' + esc(m) + '</span></div>').join('');
  let body = '';
  if (add) body += '<div style="color:var(--green);font-weight:600;margin-bottom:6px">' + t('新增模型') + '</div>' + add;
  if (rem) body += '<div style="color:var(--text2);font-weight:600;margin:10px 0 6px">' + t('已下架（保留在列表，标记显示）') + '</div>' + rem;
  if (!body) body = '<div style="color:var(--text2)">' + t('模型无变化') + '</div>';
  box.innerHTML =
    '<h2 style="margin:0 0 10px;font-size:18px">' + t('模型同步') + '</h2>' +
    '<div style="font-size:13px;max-height:320px;overflow-y:auto;color:var(--text);line-height:1.6">' + body + '</div>' +
    '<div style="text-align:right;margin-top:16px"><button class="btn btn-primary" onclick="closeModelSyncModal()">OK</button></div>';
  ov.style.display = 'flex';
}

function closeModelSyncModal() {
  const ov = _('modelSyncOverlay');
  if (ov) ov.style.display = 'none';
}

async function addModel() {
  const id = _('newModelId').value.trim();
  const cost = _('newModelCost').value;
  if (!id) { toast(t('请输入模型 ID'), 'error'); return; }
  try {
    await api('POST', '/models/add', { id, cost });
    toast(t('模型已添加: ') + id, 'success');
    _('newModelId').value = '';
    await loadModels();
    await loadConfig();
  } catch (e) { toast(t('添加失败: ') + e.message, 'error'); }
}

async function deleteModel(id) {
  if (!confirm(t('确认删除模型 ') + id + ' ?')) return;
  try {
    await api('POST', '/models/delete', { id });
    toast(t('模型已删除: ') + id, 'success');
    await loadModels();
    await loadConfig();
  } catch (e) { toast(t('删除失败: ') + e.message, 'error'); }
}

// ========== 配置加载 ==========
async function loadConfig() {
  try {
    const d = await api('GET', '/config');
    const c = d.data;
    if (c.address) _('settingAddr').value = c.address;
    if (c.strategy) _('settingStrategy').value = c.strategy;
    _('settingModelChain').value = (c.modelChain || []).join(', ');
    if (c.onlyFree !== undefined && _('settingOnlyFree')) _('settingOnlyFree').value = String(c.onlyFree);
    if (c.version) {
      if (_('settingVersion')) _('settingVersion').value = c.version;
      if (_('footerVersion')) _('footerVersion').textContent = c.version;
      if (_('aboutVersion')) _('aboutVersion').textContent = t('版本 ') + c.version;
    }
    if (c.poolPath) _('settingPoolPath').value = c.poolPath;
    if (c.defaultModel !== undefined) {
      const sel = _('settingDefModel');
      // 免费一组、付费一组（optgroup 分类 + 选项标注计费）；
      // 当前默认值不在可用列表时补在开头，避免选中值丢失
      const has = id => (_cachedModels || []).some(m => m.id === id);
      let opts = '';
      if (c.defaultModel && !has(c.defaultModel)) {
        opts += '<option value="' + esc(c.defaultModel) + '" selected>' + esc(c.defaultModel) + esc(t('（当前）')) + '</option>';
      }
      [{ key: 'free', label: t('免费模型'), tag: t('（免费）') },
       { key: 'pass', label: t('付费模型'), tag: t('（付费）') }].forEach(g => {
        // 已停用的模型不进默认模型下拉
        const items = (_cachedModels || []).filter(m => !m.disabled && (m.cost === 'free') === (g.key === 'free'));
        if (!items.length) return;
        opts += '<optgroup label="' + esc(g.label) + ' (' + items.length + ')">' +
          items.map(m => '<option value="' + esc(m.id) + '"' + (m.id === c.defaultModel ? ' selected' : '') + '>' + esc(m.id) + esc(g.tag) + '</option>').join('') +
          '</optgroup>';
      });
      sel.innerHTML = opts || '<option value="">' + t('（无可用模型）') + '</option>';
    }
    // 本机 IP 展示（监听 0.0.0.0 时局域网访问地址）
    const ips = c.localIPs || [];
    if (ips.length) {
      _('localIPsRow').style.display = '';
      _('localIPsList').innerHTML = ips.map(ip =>
        '<span class="model-tag free">' + esc(ip) + '</span>'
      ).join('');
    } else {
      _('localIPsRow').style.display = 'none';
    }
    // 监听地址下拉：补入本机 IP 选项并选中当前值
    const listenSel = _('settingListenHost');
    if (listenSel) {
      if (!listenSel.dataset.inited) {
        listenSel.dataset.inited = '1';
        let opts = '<option value="127.0.0.1">' + t('127.0.0.1（仅本机）') + '</option>';
        opts += '<option value="0.0.0.0">' + t('0.0.0.0（所有网卡）') + '</option>';
        opts += ips.map(ip => '<option value="' + esc(ip) + '">' + esc(ip) + t('（本机网卡）') + '</option>').join('');
        listenSel.innerHTML = opts;
      }
      if (c.host) listenSel.value = c.host;
    }
    // 后台密码状态
    if (c.hasPassword !== undefined) _('passwordStatus').textContent = c.hasPassword ? t('已启用') : t('未启用');
    // 非回环监听安全警告（0.0.0.0 / 局域网 IP 都会暴露管理后台）
    const h = c.host || '';
    const safeHosts = ['', '127.0.0.1', 'localhost', '::1'];
    _('listenWarn').style.display = (safeHosts.indexOf(h) === -1) ? '' : 'none';
    if (c.headers) {
      const tbody = _('headersTableBody');
      tbody.innerHTML = Object.entries(c.headers).map(([k, v]) =>
        '<tr>' +
          '<td><input type="text" class="header-key" value="' + esc(k) + '" style="font-size:12px;font-family:ui-monospace,monospace;width:100%"></td>' +
          '<td><input type="text" class="header-val" value="' + esc(v) + '" style="font-size:12px;font-family:ui-monospace,monospace;width:100%"></td>' +
          '<td><button class="btn btn-sm btn-danger" onclick="this.closest(\'tr\').remove()">✕</button></td>' +
        '</tr>'
      ).join('');
    }
  } catch (e) { /* ignore */ }
}

// ========== 请求日志 ==========
let logCursor = '';
let logHasMore = false;
let _logRowSeq = 0;

const formatDuration = ms => {
  if (!ms || ms <= 0) return '-';
  if (ms < 1000) return ms + 'ms';
  return (ms / 1000).toFixed(1) + 's';
};
const formatTPS = v => (!v || v <= 0) ? '-' : v.toFixed(1);

// 尝试链路结果标签（成功/失败/跳过）
function attemptOutcomeLabel(o) {
  if (o === 'success') return t('成功');
  if (o === 'skipped') return t('跳过');
  return t('失败');
}

// 尝试链路渲染：序号 / 模型 / 上游 / 结果 / HTTP 状态 / 耗时 / 原因
function renderAttempts(attempts) {
  return '<ol class="log-attempts">' + attempts.map((a, i) => {
    const outcome = a.outcome || 'failed';
    const oc = (outcome === 'success' || outcome === 'failed' || outcome === 'skipped') ? outcome : 'failed';
    const meta = [];
    if (a.upstream) meta.push(esc(a.upstream));
    if (a.status) meta.push('HTTP ' + esc(String(a.status)));
    if (typeof a.durationMs === 'number' && a.durationMs >= 0) meta.push(formatDuration(a.durationMs));
    return '<li>' +
      '<span class="combo-entry-idx">' + (i + 1) + '</span>' +
      '<span class="attempt-model">' + esc(a.model || '-') + '</span>' +
      '<span class="attempt-outcome ' + oc + '">' + esc(attemptOutcomeLabel(outcome)) + '</span>' +
      (meta.length ? '<span class="attempt-meta">' + meta.join(' · ') + '</span>' : '') +
      (a.reason ? '<span class="attempt-reason">' + esc(a.reason) + '</span>' : '') +
    '</li>';
  }).join('') + '</ol>';
}

// 日志模型单元格：服务模型 + 组合徽章 + 回退计数（requestedModel 与服务模型不同时一并显示）
function logModelCell(l) {
  let html = '<span class="mono" style="font-size:11px">' + esc(l.model || '-') + '</span>';
  if (l.requestedModel && l.requestedModel !== l.model) {
    html += '<span class="log-req-model">' + t('请求: ') + esc(l.requestedModel) + '</span>';
  }
  if (l.comboId) {
    html += '<span class="log-combo-badge">' + t('组合') + ': ' + esc(l.comboName || l.comboId) + '</span>';
  }
  if (l.fallbackUsed) {
    const n = Number(l.fallbackCount) || 0;
    html += '<span class="log-fallback-badge">' + t('回退') + (n > 0 ? ' ' + n : '') + '</span>';
  }
  return '<div class="log-model-cell">' + html + '</div>';
}

// 表格行展开/收起尝试链路详情行
function toggleLogDetail(id) {
  const el = _(id);
  if (!el) return;
  el.style.display = (el.style.display === 'none' || !el.style.display) ? '' : 'none';
}

async function loadRequestLogs(reset) {
  if (reset) { logCursor = ''; _logRowSeq = 0; }
  const cursor = logCursor;
  try {
    const path = '/request-logs?limit=50' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '');
    const d = await api('GET', path);
    const page = d.data;
    const items = page.items || [];
    logHasMore = !!page.hasMore;
    logCursor = page.nextCursor || '';
    _('logLoadMore').style.display = logHasMore ? 'block' : 'none';

    const tbody = _('logTableBody');
    const cards = _('logCards');
    if (reset && (!items || items.length === 0)) {
      tbody.innerHTML = '<tr><td colspan="12" class="empty">' + t('暂无请求日志') + '</td></tr>';
      cards.innerHTML = '<div class="empty">' + t('暂无请求日志') + '</div>';
      return;
    }

    const renderRow = l => {
      const ts = l.startedAt ? new Date(l.startedAt).toLocaleString(LC()) : '-';
      const st = l.completed
        ? '<span class="log-status ok">' + t('完成') + '</span>'
        : '<span class="log-status fail" title="' + esc(l.error || '') + '">' + t('失败') + '</span>';
      const tk = l.usageAvailable
        ? formatTokenCount(l.inputTokens) + '</td><td>' + formatTokenCount(l.outputTokens) + '</td><td>' + formatTokenCount(l.cachedTokens) + '</td><td>' + formatTokenCount(l.totalTokens)
        : '-</td><td>-</td><td>-</td><td>-';
      const attempts = Array.isArray(l.attempts) ? l.attempts : [];
      const hasAttempts = attempts.length > 0;
      const idx = _logRowSeq++;
      const detailId = 'logDetail' + idx;
      const rowAttr = hasAttempts
        ? ' class="log-row has-attempts" onclick="toggleLogDetail(\'' + detailId + '\')" title="' + t('点击查看尝试链路') + '"'
        : ' class="log-row"';
      return '<tr' + rowAttr + '>' +
        '<td class="mono" style="font-size:11px">' + ts + '</td>' +
        '<td>' + esc(l.accountEmail || l.upstream || '-') + '</td>' +
        '<td>' + esc(l.protocol || '-') + '</td>' +
        '<td>' + logModelCell(l) + '</td>' +
        '<td>' + tk + '</td>' +
        '<td>' + formatDuration(l.durationMs) + '</td>' +
        '<td>' + (l.ttftMs ? formatDuration(l.ttftMs) : '-') + '</td>' +
        '<td>' + formatTPS(l.outputTokensPerSecond) + '</td>' +
        '<td>' + st + '</td></tr>' +
        (hasAttempts
          ? '<tr id="' + detailId + '" class="log-detail" style="display:none"><td colspan="12" class="log-detail-cell">' + renderAttempts(attempts) + '</td></tr>'
          : '');
    };
    const renderCard = l => {
      const ts = l.startedAt ? new Date(l.startedAt).toLocaleString(LC()) : '-';
      const st = l.completed ? t('完成') : t('失败');
      const tk = l.usageAvailable
        ? t('输入 ') + formatTokenCount(l.inputTokens) + t(' · 输出 ') + formatTokenCount(l.outputTokens) + t(' · 缓存 ') + formatTokenCount(l.cachedTokens) + t(' · 总 ') + formatTokenCount(l.totalTokens)
        : t('Token 未知');
      const attempts = Array.isArray(l.attempts) ? l.attempts : [];
      let extra = '';
      if (l.comboId) extra += '<span class="log-combo-badge">' + t('组合') + ': ' + esc(l.comboName || l.comboId) + '</span>';
      if (l.fallbackUsed) {
        const n = Number(l.fallbackCount) || 0;
        extra += '<span class="log-fallback-badge">' + t('回退') + (n > 0 ? ' ' + n : '') + '</span>';
      }
      const extraBlock = extra ? '<div style="display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin-bottom:6px">' + extra + '</div>' : '';
      const reqLine = (l.requestedModel && l.requestedModel !== l.model)
        ? '<div class="log-req-model" style="margin-bottom:6px">' + t('请求: ') + esc(l.requestedModel) + '</div>' : '';
      const attemptsBlock = attempts.length
        ? '<details class="log-card-attempts"><summary>' + t('尝试链路') + ' (' + attempts.length + ')</summary>' + renderAttempts(attempts) + '</details>'
        : '';
      return '<article class="account-card">' +
        '<div class="account-card-header"><span class="account-email">' + esc(l.accountEmail || l.upstream || '-') + '</span><span class="log-status ' + (l.completed ? 'ok' : 'fail') + '">' + st + '</span></div>' +
        '<div class="account-metrics">' +
          '<div class="account-metric"><span class="account-metric-label">' + t('协议') + '</span><span class="account-metric-value">' + esc(l.protocol || '-') + '</span></div>' +
          '<div class="account-metric"><span class="account-metric-label">' + t('耗时') + '</span><span class="account-metric-value">' + formatDuration(l.durationMs) + '</span></div>' +
          '<div class="account-metric"><span class="account-metric-label">TTFT</span><span class="account-metric-value">' + (l.ttftMs ? formatDuration(l.ttftMs) : '-') + '</span></div>' +
          '<div class="account-metric"><span class="account-metric-label">tok/s</span><span class="account-metric-value">' + formatTPS(l.outputTokensPerSecond) + '</span></div>' +
        '</div>' +
        extraBlock + reqLine +
        '<div style="font-size:12px;color:var(--text2);margin-bottom:8px">' + tk + '</div>' +
        '<div class="account-card-footer"><span class="mono" style="font-size:11px">' + ts + '</span><span class="mono" style="font-size:11px">' + esc(l.model || '-') + '</span></div>' +
        attemptsBlock +
      '</article>';
    };

    if (reset) {
      tbody.innerHTML = items.map(renderRow).join('');
      cards.innerHTML = items.map(renderCard).join('');
    } else {
      tbody.insertAdjacentHTML('beforeend', items.map(renderRow).join(''));
      cards.insertAdjacentHTML('beforeend', items.map(renderCard).join(''));
    }
  } catch (e) { toast(t('加载日志失败: ') + e.message, 'error'); }
}

// ========== 初始化 ==========
applyLang();
// 恢复上次打开的标签页（URL hash 优先，其次 sessionStorage），并加载该页数据
const bootTab = getActiveTab();
applyTab(bootTab);
// 与标签页无关的全局预热：密钥列表、模型列表 + 配置
loadKeys();
loadModels().then(() => loadConfig());
setInterval(() => { loadStats(); }, 10000);
// 手动改 hash / 浏览器前进后退时同步切换标签页
window.addEventListener('hashchange', () => applyTab(getActiveTab()));
</script>
</body>
</html>`
