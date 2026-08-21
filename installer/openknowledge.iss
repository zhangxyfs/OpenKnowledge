; OpenKnowledge 安装程序脚本（Inno Setup 6/7）
; 构建：bash scripts/build-installer.sh（先构建 dist/ 再调用 ISCC）

#define AppName "OpenKnowledge"
#define AppVersion "2.21.0"
#define AppPublisher "OpenKnowledge"

[Setup]
AppId={{9F4C3A2E-7B1D-4A5F-9E2C-6D8B1A3F5E70}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
DefaultDirName={localappdata}\Programs\OpenKnowledge
UsePreviousAppDir=no
DefaultGroupName=OpenKnowledge
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
OutputDir=output
OutputBaseFilename=OpenKnowledgeSetup-{#AppVersion}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
UninstallDisplayName={#AppName} 知识库
SetupIconFile=assets\logo.ico
; 数据目录 ~/.openknowledge 由程序运行时创建，卸载默认保留（见 [Code]）

[Languages]
Name: "chinesesimplified"; MessagesFile: "lang\ChineseSimplified.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "附加任务："
Name: "addpath"; Description: "将安装目录加入用户 PATH（终端可直接使用 ok 命令）"; GroupDescription: "附加任务："; Flags: unchecked

[Files]
Source: "..\dist\ok.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\okd.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\OkManager.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\web\*"; DestDir: "{app}\web"; Flags: ignoreversion recursesubdirs
Source: "..\dist\changelogs\*"; DestDir: "{app}\changelogs"; Flags: ignoreversion recursesubdirs
Source: "..\dist\runtime\*"; DestDir: "{app}\runtime"; Flags: ignoreversion recursesubdirs
Source: "assets\logo.ico"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\OpenKnowledge 知识库"; Filename: "{app}\OkManager.exe"; IconFilename: "{app}\logo.ico"; Comment: "打开 OpenKnowledge 配置中心"
Name: "{group}\卸载 OpenKnowledge"; Filename: "{uninstallexe}"
Name: "{autodesktop}\OpenKnowledge 知识库"; Filename: "{app}\OkManager.exe"; IconFilename: "{app}\logo.ico"; Tasks: desktopicon

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "OpenKnowledge"; ValueData: """{app}\okd.exe"""; Flags: uninsdeletevalue

[Run]
Filename: "{app}\OkManager.exe"; Description: "打开 OpenKnowledge 配置中心（引导页可一键完成 hooks / 技能 / embedding 配置）"; Flags: postinstall skipifsilent unchecked

[Code]
const
  UninstallKey = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{9F4C3A2E-7B1D-4A5F-9E2C-6D8B1A3F5E70}_is1';
  EnvKey = 'Environment';

{ 升级判定：存在旧安装且旧版本号 ≠ 当前版本号 }
function IsUpgrade: Boolean;
var
  PrevVer: string;
begin
  Result := RegQueryStringValue(HKCU, UninstallKey, 'DisplayVersion', PrevVer) and
            (PrevVer <> '{#AppVersion}');
end;

{ 有旧安装时预填旧目录（UsePreviousAppDir=no 下由代码接管默认值） }
procedure InitializeWizard;
var
  PrevDir: string;
begin
  if RegQueryStringValue(HKCU, UninstallKey, 'InstallLocation', PrevDir) and (PrevDir <> '') then
    WizardForm.DirEdit.Text := PrevDir;
end;

{ 升级时跳过目录选择页（静默装进旧目录）；同版本重装/首次安装仍显示 }
function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := (PageID = wpSelectDir) and IsUpgrade;
end;

function PathContains(const Path, Dir: string): Boolean;
begin
  Result := Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(Path) + ';') > 0;
end;

procedure AddToUserPath(const Dir: string);
var
  Path: string;
begin
  if not RegQueryStringValue(HKCU, EnvKey, 'Path', Path) then
    Path := '';
  if not PathContains(Path, Dir) then
  begin
    if (Path <> '') and (Path[Length(Path)] <> ';') then
      Path := Path + ';';
    Path := Path + Dir;
    RegWriteStringValue(HKCU, EnvKey, 'Path', Path);
  end;
end;

procedure RemoveFromUserPath(const Dir: string);
var
  Path, UpperDir, UpperPath: string;
  P: Integer;
begin
  if not RegQueryStringValue(HKCU, EnvKey, 'Path', Path) then
    exit;
  UpperDir := Uppercase(Dir);
  UpperPath := Uppercase(Path);
  P := Pos(';' + UpperDir + ';', ';' + UpperPath + ';');
  while P > 0 do
  begin
    Delete(Path, P, Length(Dir) + 1);
    if (P <= Length(Path)) and (Path[P] = ';') then
      Delete(Path, P, 1);
    UpperPath := Uppercase(Path);
    P := Pos(';' + UpperDir + ';', ';' + UpperPath + ';');
  end;
  RegWriteStringValue(HKCU, EnvKey, 'Path', Path);
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
begin
  if CurStep = ssInstall then
    { 文件拷贝前停常驻 daemon（hooks 会按需自动拉起它锁住 okd.exe；不存在则 Exec 失败无害） }
    Exec(ExpandConstant('{app}\okd.exe'), 'stop', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  if (CurStep = ssPostInstall) and WizardIsTaskSelected('addpath') then
    AddToUserPath(ExpandConstant('{app}'));
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  DataDir: string;
  ResultCode: Integer;
begin
  if CurUninstallStep = usUninstall then
  begin
    { 文件删除前停常驻 daemon（不存在则 okd.exe 立即返回 0，无害） }
    Exec(ExpandConstant('{app}\okd.exe'), 'stop', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  end;
  if CurUninstallStep = usPostUninstall then
  begin
    RemoveFromUserPath(ExpandConstant('{app}'));
    DataDir := ExpandConstant('{userdocs}\..\.openknowledge');
    { 静默卸载（/VERYSILENT）下绝不删除数据；交互模式才询问。
      注意：卸载期只能用 UninstallSilent，WizardSilent 是 Setup 期函数，误用会运行时错误。 }
    if (not UninstallSilent) and DirExists(DataDir) then
    begin
      if MsgBox('是否同时删除知识库数据？' + #13#10 + #13#10 +
                DataDir + #13#10 +
                '（包含全部知识条目、索引与配置。选"否"保留，重装后可继续使用。）',
                mbConfirmation, MB_YESNO) = IDYES then
        DelTree(DataDir, True, True, True);
    end;
  end;
end;
