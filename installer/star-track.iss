; 星记 Inno Setup 安装脚本
; 由 GitHub Actions 调用 iscc 编译为 星记-Setup.exe

#define AppName       "星记"
#define AppNameEN     "StarTrack"
#define AppVersion    "1.0.0"
#define AppPublisher  "星轨"
#define AppExeName    "star-track-desktop.exe"

[Setup]
AppId={{8F7B5E2A-3D6F-4A8B-9C2E-1A5B4C7D9E0F}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
DefaultDirName={userdocs}\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
OutputDir=..
OutputBaseFilename=StarTrack-Setup
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=lowest
UninstallDisplayIcon={app}\{#AppExeName}
UninstallDisplayName={#AppName}
SetupIconFile=icon.ico

[Languages]
Name: "chinesesimp"; MessagesFile: "ChineseSimplified.isl"

[Tasks]
Name: "desktopicon"; Description: "在桌面创建快捷方式"; GroupDescription: "附加选项:"

[Files]
Source: "..\star-track-desktop.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\{#AppName}"; Filename: "{app}\{#AppExeName}"
Name: "{group}\卸载 {#AppName}"; Filename: "{uninstallexe}"
Name: "{userdesktop}\{#AppName}"; Filename: "{app}\{#AppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#AppExeName}"; Description: "立即启动 {#AppName}"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
; 只清理程序运行时数据，不碰根目录
Type: filesandordirs; Name: "{app}\data"
Type: filesandordirs; Name: "{app}\logs"
Type: files; Name: "{app}\.env"
Type: files; Name: "{app}\resource.syso"
Type: files; Name: "{app}\star-track-desktop.exe"

[Code]
function InitializeSetup(): Boolean;
begin
  Result := True;
end;

// 卸载时关闭进程
function InitializeUninstall(): Boolean;
var
  ResultCode: Integer;
begin
  Exec(ExpandConstant('{cmd}'), '/C taskkill /IM star-track-desktop.exe /F /T', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Result := True;
end;

// 卸载完成后尝试删除 {app} 目录，但不强制（非空时自动跳过）
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
  begin
    if RemoveDir(ExpandConstant('{app}')) then
      Log('成功删除安装目录')
    else
      Log('安装目录非空或已不存在，跳过删除');
  end;
end;
