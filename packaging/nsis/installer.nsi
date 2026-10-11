; ============================================================================
; SHEYTAN-LA — SHEYTAN Local Agent — Windows installer (NSIS, v1.2.1)
; ============================================================================
; Built by CI (.github/workflows/build-desktop.yml) on the windows runner:
;
;   makensis -DVERSION=<ver> -DEXE=SHEYTAN-LA.exe \
;            -DBUILDDIR=<ABSOLUTE staging dir> \
;            -DOUTFILE=<ABSOLUTE installer path> \
;            packaging\nsis\installer.nsi
;
; PATH CONTRACT (v1.2.0 — exactly ONE convention):
;   makensis resolves compile-time paths (File, OutFile, icons) relative
;   to the .nsi SCRIPT directory, not the invoking working directory. The
;   v1.2.0 CI run passed a repo-root-relative BUILDDIR, makensis looked
;   for packaging\nsis\dist\windows\... and failed with "no files found".
;   CI now passes ABSOLUTE BUILDDIR / OUTFILE paths, which are unambiguous
;   from any invocation context. The defaults below remain script-relative
;   so a developer can build the installer from a checkout with no flags:
;
;   cd packaging/nsis && makensis installer.nsi
;
; Compile-time assertions below refuse to build anything when the staging
; executable or directory is missing, so a path-contract break fails the
; installer build immediately with a precise message instead of a generic
; "no files found".
;
; v1.2.1 CONTRACT UPGRADES:
;   - Desktop shortcut is now a GENUINE INSTALLER OPTION: a checkbox on
;     the directory page ("Create a &desktop shortcut"), DEFAULT CHECKED.
;     Unchecked → no desktop shortcut is created; Checked → created.
;   - Clean upgrades: a running instance is closed before replacing the
;     executable, with a bounded retry and an explicit Retry/Cancel
;     message if the binary is still locked. No silent partial upgrade.
;   - Uninstaller hardening: removes the application, shortcuts (Start
;     Menu + desktop), installer registration and the AppUserModelID —
;     and preserve user data by CONSTRUCTION (no recursive deletes):
;     models/, workspace/, sessions/ and configuration survive uninstall
;     untouched -- since v1.3.6 the canonical data root is the
;     install-local <AppRoot>\data tree (never an environment variable),
;     plus any explicit SHEYTAN_DATA_DIR override the user chose.
;   - Per-machine shell context for shortcuts (SetShellVarContext all),
;     so the desktop/Start Menu entries and their removal are symmetric
;     for every user of the machine.
;
; Directory selection is unchanged (MUI_PAGE_DIRECTORY) and the selected
; directory is genuinely used: every payload file, shortcut and registry
; entry below resolves through $INSTDIR.
;
; Contract:
;   - installs the APPLICATION ONLY (exe + license + readme); models are
;     NEVER bundled and NEVER deleted;
;   - per-machine install to $PROGRAMFILES64\SHEYTAN-LA;
;   - v1.3.6: the canonical data root is the INSTALL-LOCAL data tree
;     (<AppRoot>\data) created and made user-writable by the installer;
;     the legacy machine SHEYTAN_DATA_DIR environment variable is
;     REMOVED (1.3.5 pointed it at AppData — the second-data-root
;     defect). An explicit user-chosen SHEYTAN_DATA_DIR override stays
;     supported and is never rewritten; the runtime migrates old
;     AppData data into the canonical root on first run;
;   - AppUserModelID Parsaetak.SHEYTAN-LA registered under HKCU classes so
;     taskbar/notifications resolve the identity;
;   - version-aware upgrades, clean uninstall, user data untouched;
;   - NO Authenticode claim: signing runs later in CI when credentials
;     exist (signtool step is additive, not part of this script).
; ============================================================================

Unicode true
ManifestDPIAware true

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!ifndef EXE
  !define EXE "SHEYTAN-LA.exe"
!endif
; Staging directory holding the packaged application. Script-relative by
; default (packaging\nsis\..\..\dist = the repository dist directory); CI
; passes an ABSOLUTE path (see path contract above). v1.2.1 fixes the
; default: it previously pointed one level short of the repository root
; (packaging\nsis\..\dist), where no staging tree ever existed — the
; compile-time assertion below now catches that class of mistake instead
; of failing with a generic "no files found".
!ifndef BUILDDIR
  !define BUILDDIR "..\..\dist\windows\app\SHEYTAN-LA"
!endif
; Installer output path. Script-relative by default; CI passes an ABSOLUTE
; path. Derived from the same release identity the workflow and the
; stress contract use (SHEYTAN-LA-v<ver>-windows-x64-installer.exe).
!ifndef OUTFILE
  !define OUTFILE "..\..\dist\SHEYTAN-LA-v${VERSION}-windows-x64-installer.exe"
!endif

; --- compile-time assertions: fail fast, fail precisely --------------------
; NSIS 3 compile-time /FileExists with unary ! (not): the installer build
; aborts with the exact missing path instead of a per-File "no files
; found" error further down.
!if ! /FileExists "${BUILDDIR}\${EXE}"
  !error "NSIS: staging executable not found: ${BUILDDIR}\${EXE} (build the portable application first; pass an ABSOLUTE -DBUILDDIR from CI)"
!endif

!include "MUI2.nsh"
!include "FileFunc.nsh"
!include "nsDialogs.nsh"
!include "WinMessages.nsh"

!define PRODUCT       "SHEYTAN-LA"
!define DESCRIPTION   "SHEYTAN Local Agent"
!define PUBLISHER     "Parsaetak"
!define AUMID         "Parsaetak.SHEYTAN-LA"
!define REGKEY        "Software\Parsaetak\SHEYTAN-LA"
!define UNINSTKEY     "Software\Microsoft\Windows\CurrentVersion\Uninstall\SHEYTAN-LA"

; --- installer options state ------------------------------------------------
; CreateDesktopShortcut carries ${BST_CHECKED} / ${BST_UNCHECKED} across
; page transitions. It is initialised CHECKED in .onInit, bound to the
; checkbox on the directory page (DirectoryPageShow) and consumed by the
; install section. A silent install (/S) never shows the page and keeps
; the default CHECKED behaviour.
Var DesktopCheckbox
Var CreateDesktopShortcut

Name "${DESCRIPTION} v${VERSION}"
OutFile "${OUTFILE}"
InstallDir "$PROGRAMFILES64\SHEYTAN-LA"
InstallDirRegKey HKLM "${REGKEY}" "InstallDir"
RequestExecutionLevel admin
SetCompressor /SOLID lzma

VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "${PRODUCT}"
VIAddVersionKey "FileDescription" "${DESCRIPTION}"
VIAddVersionKey "CompanyName" "${PUBLISHER}"
VIAddVersionKey "InternalName" "${PRODUCT}"
VIAddVersionKey "OriginalFilename" "SHEYTAN-LA-v${VERSION}-windows-x64-installer.exe"
VIAddVersionKey "FileVersion" "${VERSION}.0"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "(c) 2024-2026 Parsaetak. All rights reserved."

!define MUI_ICON   "..\..\build\sheytan.ico"
!define MUI_UNICON "..\..\build\sheytan.ico"
!define MUI_ABORTWARNING

; The directory page stays the graphical folder-selection UI; the custom
; SHOW callback adds the desktop-shortcut checkbox to that same page, so
; the installer stays a two-click experience (directory → install).
!define MUI_PAGE_CUSTOMFUNCTION_SHOW DirectoryPageShow
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\${EXE}"
!define MUI_FINISHPAGE_RUN_TEXT "Launch ${DESCRIPTION}"
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Function .onInit
  ; Default CHECKED: the checkbox state variable, not the UI element, is
  ; the single source of truth for the section below. With no page shown
  ; (silent install) or before first render, the default applies.
  StrCpy $CreateDesktopShortcut ${BST_CHECKED}
FunctionEnd

; DirectoryPageShow runs with the live dialog every time the page is
; shown (first visit and Back/Next revisits), so the checkbox reflects
; the persisted choice instead of resetting.
Function DirectoryPageShow
  ${NSD_CreateCheckbox} 0u -34u 100% 8u "Create a &desktop shortcut"
  Pop $DesktopCheckbox
  ${NSD_SetState} $DesktopCheckbox $CreateDesktopShortcut
  ${NSD_OnClick} $DesktopCheckbox DesktopShortcutClick
FunctionEnd

; DesktopShortcutClick persists the user's choice the moment the box is
; toggled; the install section only reads the variable.
Function DesktopShortcutClick
  Pop $0 ; control HWND pushed by nsDialogs::OnClick
  ${NSD_GetState} $DesktopCheckbox $CreateDesktopShortcut
FunctionEnd

Section "Install"
  SetOutPath "$INSTDIR"

  ; --- clean upgrade: replace a running instance safely -------------------
  ; The GUI binary is write-locked while the app runs. Ask Windows to
  ; close it (WM_CLOSE first, then force), then verify the lock is gone
  ; with a BOUNDED retry (3 × 500 ms) before touching anything. If it is
  ; still locked the user gets Retry/Cancel — never a silent partial
  ; upgrade, never an uncontrolled loop.
  DetailPrint "Closing any running ${DESCRIPTION} instance..."
  nsExec::Exec 'taskkill /IM "${EXE}"'
  Sleep 300
  nsExec::Exec 'taskkill /IM "${EXE}" /F'
  Sleep 200

  StrCpy $R0 0
upgrade_retry:
  ClearErrors
  Delete "$INSTDIR\${EXE}"
  IfErrors 0 upgrade_replaced
  IntOp $R0 $R0 + 1
  IntCmp $R0 3 0 upgrade_retry_sleep upgrade_retry_sleep
  MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION \
    "${DESCRIPTION} is still running and its executable is locked.$\n$\nClose ${DESCRIPTION} and click Retry to continue the upgrade." \
    IDRETRY upgrade_retry
  Abort "Upgrade aborted: $INSTDIR\${EXE} is locked by a running instance."
upgrade_retry_sleep:
  Sleep 500
  Goto upgrade_retry
upgrade_replaced:

  ; --- application payload (never models) ---------------------------------
  File "${BUILDDIR}\${EXE}"
  File /nonfatal "${BUILDDIR}\LICENSE.md"
  File /nonfatal "${BUILDDIR}\README.md"
  File /nonfatal "${BUILDDIR}\SIGNATURE"
  File "..\..\build\sheytan.ico"

  ; Version-aware upgrade bookkeeping.
  WriteRegStr HKLM "${REGKEY}" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "${REGKEY}" "Version" "${VERSION}"
  WriteRegStr HKLM "${REGKEY}" "Publisher" "${PUBLISHER}"

  ; User data location — v1.3.6 CANONICAL DATA ROOT CONTRACT.
  ;
  ; The runtime resolves its canonical data root to the installation
  ; root: <AppRoot>\data\ (models/, bin/, sessions/, logs/, ...) unless
  ; the user sets an EXPLICIT SHEYTAN_DATA_DIR override. The installer
  ; therefore:
  ;
  ;   1. creates the install-local data tree and grants the built-in
  ;      Users group MODIFY rights (icacls, inheritable) -- the selected
  ;      installation root under Program Files is admin-writable only,
  ;      and mutable models/logs/sessions MUST stay writable for the
  ;      running application (spec section 17: no protected-directory
  ;      hope);
  ;   2. records the data root under the product key for support
  ;      diagnostics (NOT as an environment variable);
  ;   3. REMOVES the legacy machine SHEYTAN_DATA_DIR environment
  ;      variable -- 1.3.5 installers set it to AppData, which is
  ;      exactly the second-data-root defect this release removes. On
  ;      first run after the upgrade, the runtime migrates the old
  ;      AppData data into <AppRoot>\data once (hash-verified,
  ;      restart-safe) and the legacy root is retired.
  CreateDirectory "$INSTDIR\data"
  CreateDirectory "$INSTDIR\data\models"
  CreateDirectory "$INSTDIR\data\bin"
  CreateDirectory "$INSTDIR\data\sessions"
  CreateDirectory "$INSTDIR\data\logs"
  CreateDirectory "$INSTDIR\data\workspace"
  nsExec::ExecToLog 'icacls "$INSTDIR\data" /grant *S-1-5-32-545:(OI)(CI)M'
  Pop $0

  WriteRegStr HKLM "${REGKEY}" "DataDir" "$INSTDIR\data"
  DeleteRegValue HKLM "SYSTEM\CurrentControlSet\Control\Session Manager\Environment" "SHEYTAN_DATA_DIR"
  ; v1.3.7: broadcast the environment change so already-running processes
  ; (and the shell that launches them) observe the deletion instead of
  ; inheriting the stale SHEYTAN_DATA_DIR=%LOCALAPPDATA%\SHEYTAN-LA into
  ; children -- a stale value resolves as an "explicit override", which
  ; skips every migration and recreates the retired AppData root for one
  ; launch window.
  SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000

  ; Windows application identity (AppUserModelID) — notifications and
  ; taskbar grouping resolve to SHEYTAN-LA.
  WriteRegStr HKCU "Software\Classes\AppUserModelId\${AUMID}" "DisplayName" "${DESCRIPTION}"
  WriteRegStr HKCU "Software\Classes\AppUserModelId\${AUMID}" "IconUri" "$INSTDIR\sheytan.ico"

  ; Per-machine shell context: shortcuts land on the common desktop and
  ; the all-users Start Menu, so every user of the machine sees them and
  ; the uninstaller removes exactly what the installer created.
  SetShellVarContext all

  ; Start Menu shortcuts.
  CreateDirectory "$SMPROGRAMS\${PRODUCT}"
  CreateShortcut "$SMPROGRAMS\${PRODUCT}\${DESCRIPTION}.lnk" "$INSTDIR\${EXE}" "" "$INSTDIR\sheytan.ico"
  CreateShortcut "$SMPROGRAMS\${PRODUCT}\Uninstall ${DESCRIPTION}.lnk" "$INSTDIR\Uninstall.exe"

  ; Optional desktop shortcut — GENUINE OPTION (v1.2.1). The checkbox on
  ; the directory page (default CHECKED) drives this decision:
  ;   checked   → desktop shortcut created
  ;   unchecked → no desktop shortcut created
  IntCmp $CreateDesktopShortcut ${BST_CHECKED} 0 skip_desktop skip_desktop
  CreateShortcut "$DESKTOP\${DESCRIPTION}.lnk" "$INSTDIR\${EXE}" "" "$INSTDIR\sheytan.ico"
skip_desktop:

  ; Add/Remove Programs entry.
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayName" "${DESCRIPTION} v${VERSION}"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINSTKEY}" "Publisher" "${PUBLISHER}"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\sheytan.ico"
  WriteRegStr HKLM "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${UNINSTKEY}" "UninstallString" "$INSTDIR\Uninstall.exe"
  WriteRegDWORD HKLM "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINSTKEY}" "NoRepair" 1

  ; Installed size for ARP.
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKLM "${UNINSTKEY}" "EstimatedSize" $0

  WriteUninstaller "$INSTDIR\Uninstall.exe"
SectionEnd

Section "Uninstall"
  ; Per-machine shell context — the exact mirror of the install section,
  ; so the shortcuts removed here are the ones the installer created.
  SetShellVarContext all

  ; Remove the application — and NOTHING else.
  ;
  ; preserve user data — models/, sessions/, logs/ and configuration
  ; live under the install-local <AppRoot>\data root (v1.3.6) or a
  ; user-chosen SHEYTAN_DATA_DIR override, and are deliberately
  ; PRESERVED. Users who keep models inside $INSTDIR are
  ; equally safe: plain RMDir (never its recursive variant) removes the
  ; directory only when it is EMPTY, so anything left behind stays on
  ; disk untouched. Recursion is forbidden here by the CI contract on
  ; this very file.
  Delete "$INSTDIR\${EXE}"
  Delete "$INSTDIR\LICENSE.md"
  Delete "$INSTDIR\README.md"
  Delete "$INSTDIR\SIGNATURE"
  Delete "$INSTDIR\sheytan.ico"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\${PRODUCT}\${DESCRIPTION}.lnk"
  Delete "$SMPROGRAMS\${PRODUCT}\Uninstall ${DESCRIPTION}.lnk"
  RMDir "$SMPROGRAMS\${PRODUCT}"

  ; Desktop shortcut is removed only if it exists — Delete on a missing
  ; file is a no-op, so an install made with the option unchecked is
  ; handled by the same instruction.
  Delete "$DESKTOP\${DESCRIPTION}.lnk"

  ; Installer registration + application identity.
  DeleteRegKey HKCU "Software\Classes\AppUserModelId\${AUMID}"
  DeleteRegKey HKLM "${UNINSTKEY}"

  ; Keep HKLM "${REGKEY}" so a repaired install remembers the directory.
  ReadRegStr $0 HKLM "${REGKEY}" "InstallDir"
  StrCmp $0 "" 0 +2
    DeleteRegKey HKLM "${REGKEY}"

  ; NOTE (v1.3.6): the machine SHEYTAN_DATA_DIR variable is DELETED by
  ; the install section (the 1.3.5 AppData contract is retired). User
  ; data under the install-local <AppRoot>\data root is preserved by the
  ; plain-RMDir rule above; a user-chosen SHEYTAN_DATA_DIR override is
  ; never touched by either installer or uninstaller.
SectionEnd
