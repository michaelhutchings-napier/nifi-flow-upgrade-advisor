mod commands;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    configure_runtime_environment();

    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![
            commands::bootstrap_state,
            commands::scan_workspace,
            commands::run_cli_action,
            commands::read_text_file,
            commands::open_path
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

fn configure_runtime_environment() {
    #[cfg(target_os = "linux")]
    {
        // Default to software rendering on Linux unless the user explicitly chose
        // a GL mode already. This keeps headless CI and low-GPU desktops quieter
        // and more reliable with WebKitGTK/Tauri.
        if std::env::var_os("LIBGL_ALWAYS_SOFTWARE").is_none() {
            unsafe {
                std::env::set_var("LIBGL_ALWAYS_SOFTWARE", "1");
            }
        }
    }
}
