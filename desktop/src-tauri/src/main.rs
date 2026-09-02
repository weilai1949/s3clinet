// s3clinet 桌面壳：纯 B/S 架构，不使用任何 Tauri IPC。
// 仅创建一个系统 WebView，加载 web 前端，前端通过 HTTP 访问 Go 后端
// 并直接使用 S3 v4 签名 URL 直传。此处没有任何 invoke_handler / 自定义 command。
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

fn main() {
    tauri::Builder::default()
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
