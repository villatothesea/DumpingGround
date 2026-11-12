package startup

import "strings"

// Advice is what the Suggestion column shows for a startup entry: a
// level (keep / optional / safe to disable / be careful) plus the
// consequence that switching it off has. Reason is always one of the
// shared r* keys — a localized sentence — so every row explains itself
// in the UI language without needing a per-item translation.
type Advice struct {
	Level  int
	Reason string
}

const (
	AdviceUnknown  = iota // not in the tables — research before touching
	AdviceKeep            // disabling degrades something real
	AdviceOptional        // fine either way — off if you don't use it daily
	AdviceSafeOff         // helper/update/telemetry noise — app still works
	AdviceCaution         // hardware or system machinery — know why first
)

// Consequence texts, one per consequence class — dozens of entries share
// each sentence instead of carrying their own.
const (
	rCore      = "Windows core component — disabling breaks system features"
	rUpdater   = "Only checks for updates — the app still works, updates need a manual run or happen at launch"
	rTelemetry = "Telemetry and diagnostics upload — nothing functional is lost"
	rHelper    = "Background helper/tray icon — the app still starts when you open it"
	rSync      = "Cloud sync resident — off means no automatic sync; open the client to sync"
	rPreload   = "Preloader that makes the app open faster — off means a slower first launch"
	rDriver    = "Hardware companion service — disabling can break the related device feature"
	rGame      = "Game platform resident — start it yourself when you play"
	rPrint     = "Printing support — safe to switch off if this machine never prints"
	rIndex     = "Builds the search index — off makes file search slower, not broken"
	rProxy     = "Proxy/VPN resident — traffic stops flowing through it until you start it"
	rRemote    = "Remote-access helper — off means this machine cannot be reached remotely"
	rSecurity  = "Security software — keep it unless you have a replacement protecting the machine"
	rIM        = "Messaging app — off means no new-message notifications until you open it"
	rWinOpt    = "Optional Windows feature service — usually fine off unless a feature you use relies on it"
	rMedia     = "Media/network sharing — off if this machine doesn't stream to other devices"
	rBluetooth = "Bluetooth support — off breaks pairing and Bluetooth audio"
	rInput     = "Input method — off breaks typing in that IME"
	rRuntime   = "Shared runtime other software may depend on — disabling can break dependents"
	rServer    = "Background server — just holds RAM while idle; start it when you actually use it"
	rBackup    = "Backup agent — switching it off stops scheduled backups"
	rCosmetic  = "Cosmetic tray/companion — convenience only, nothing breaks"
	rVM        = "Virtualization support — the related VMs/containers stop working until it runs again"
	rCustomUI  = "Desktop customization — switching it off reverts the appearance it provides"
	rOnDemand  = "On-demand tool — start it when you need it"
	rUnused    = "Optional extra — off if you never use it"
	rCompanion = "Vendor companion — off if you don't use its features"
	rOnlyIf    = "Needed only in its specific scenario — off if that doesn't apply"
	rMailOff   = "Mail client — off stops new-mail alerts"
	rPackaged  = "Managed by an app package — Windows may re-enable it; use the packaged startup item or Windows Settings → Apps → Startup"
	rJunkSoft  = "Bundled pushware — off is a good start"
	rUnknown   = "No information on record — check what it does before switching it"
	rWinUnk    = "A Windows component not on the known list — leave it on unless you know it"
)

// Item-specific consequence texts for widely-installed software. Where a
// shared r* line would be too vague, these say what the component does and
// what switching it off actually loses.
const (
	dSecHealth   = "Windows Security's tray icon — protection continues without it; off only removes the shield icon and its status warnings"
	dRtk         = "Realtek audio companion — basic sound usually survives, but jack detection, EQ and the audio console can stop; a few systems lose audio entirely"
	dLogiUpd     = "Logitech's update checker — mice and keyboards keep working; off just stops the periodic driver-update prompts"
	dUpdAssist   = "Vendor download/update assistant — the device or app keeps working; off just stops the periodic update prompts"
	dOneDrive    = "OneDrive's sync client — off stops files syncing to the cloud; local copies stay put, open it when you want to sync"
	dOneDriveUpd = "OneDrive's own updater — sync keeps working; off only stops silent client updates"
	dEdgePreload = "Edge's preloader — opens a hidden browser at sign-in for a faster first launch; off just makes the first open slower"
	dCollabSync  = "Acrobat's cloud-document sync — local PDF viewing and editing are unaffected; cloud files sync only while Acrobat is open"
	dClickToRun  = "Office's launch and update service — Word, Excel and PowerPoint start through it; turning it off breaks Office"
	dDocker      = "Docker's Linux VM backend — idles on real RAM (often 1 GB+); off means docker commands do nothing until you start it"
	dWslSvc      = "Windows Subsystem for Linux service — WSL distros and WSL-based Docker cannot start while it is off"
	dTailscale   = "Tailscale mesh VPN — off removes this machine from your tailnet; peers can't reach it and it can't reach them"
	dClashApp    = "Clash Verge proxy client — off sends all traffic direct; system proxy and TUN mode stop until you reopen it"
	dClashSvc    = "Privileged helper for Clash's TUN/enhanced mode — plain system-proxy mode doesn't need it; off only breaks TUN mode"
	dSnipaste    = "Snipaste screenshot/pin tool — off means its capture hotkey and screen pins do nothing until you launch it"
	dDingTalk    = "DingTalk work messenger — off means no message notifications and no auto sign-in until you open it"
	dMagpie      = "Magpie window scaler — off means its scaling hotkey does nothing; open it when you need to upscale a window or game"
	dWarp        = "Warp terminal — pure convenience autostart; nothing needs it until you open a terminal"
	dGameViewer  = "NetEase UU remote-play service — streams this PC's games to other devices; off if you don't remote-play"
	dQuarkUpd    = "Quark cloud drive's updater — the drive still works; off just delays version updates until you install them"
	dAppUpd      = "Background updater — the app itself still works; off just stops silent version updates"
	dLenovoPCM   = "Lenovo PC Manager's backend — fan curves, eye-care and Lenovo popups; off returns thermal control to Windows defaults, hardware unaffected"
	dLenovoPM    = "Lenovo power-management monitor — feeds its battery/power features; off loses those, nothing else"
	dMsPCM       = "Microsoft PC Manager's resident component — drives its cleanup/boost prompts; off only silences them, Windows is unaffected"
	dPCMgr       = "PC-manager suite's resident component — its cleanup/boost/toolbox features stop; the system itself is unaffected"
	dIntelGfx    = "Intel Graphics Software companion — game profiles, tuning and driver-update notices; the GPU driver itself is unaffected"
	dSynaptics   = "Synaptics touchpad settings component — taps and gestures keep working via the driver; off just stops its settings app autostarting"
	dYourPhone   = "Phone Link — off means no phone notifications, texts or photos on this PC until you open it"
	dStartFeed   = "Microsoft Start's feed provider — powers the widgets news/weather panel; off just empties the feed"
	dToDo        = "Microsoft To Do — off only stops it launching at sign-in; open it from Start when needed"
	dTerminal    = "Windows Terminal's login autostart — off stops a terminal window appearing at every sign-in"
	dPowerAuto   = "Power Automate Desktop — needed for unattended RPA flows to run on schedule; off if you don't automate tasks"
	dOfficeUpd   = "Office's updater task — the apps keep working; off means updates wait until triggered from an app's Account page"
	dOneNoteTool = "OneNote's quick-note tray tool — off removes the Win+N quick-note hotkey and tray icon; OneNote itself is unaffected"
	dOfficeTel   = "Office telemetry agent — uploads usage data to Microsoft; off loses nothing functional"
	dOspp        = "Office licensing service — keeps activated Office activated; off can un-activate or break Office"
	dOfficeSvc   = "Microsoft Office Service — the subscription heartbeat for Microsoft 365; off can break subscription checks and updates"
	dOfficeBG    = "Office background-task registrar — re-registers Office's maintenance tasks; off can leave them unscheduled"
	dOse         = "Office Source Engine — serves install/repair source files; off breaks online repair and uninstall of Office"
	dLync        = "Skype for Business — enterprise IM and meetings; off means no presence or messages until opened"
	dGroove      = "SharePoint Workspace (Groove) sync — the retired Office sync tool; nothing current uses it"
	dOutlook     = "Outlook — off stops new-mail alerts and calendar reminders until you open it"
	dBrowserUpd  = "Browser updater — the browser still works; off means updates apply when it launches instead of in the background"
	dChatGPT     = "ChatGPT desktop app — off removes the tray icon and global hotkey; launch it from Start when needed"
	dCodexSvc    = "ChatGPT's sandbox service for Codex runs — the packaged 'ChatGPT' startup item is the durable switch; package updates can re-enable this service"
	dXbox        = "Xbox Gaming App's background component — off only stops Game Bar and store features preloading; installed games still run"
)

// adviceServices is keyed by the lowercase service name — the tail of
// Location's "Service <name>". It covers the stock Windows 11 auto-start
// set plus common OEM and driver companions.
var adviceServices = map[string]Advice{
	// Windows core — keep
	"audioendpointbuilder":         {AdviceKeep, "Windows Audio Endpoint Builder — detects and manages audio devices; without it no audio device works"},
	"audiosrv":                     {AdviceKeep, "Windows Audio — the sound engine itself; without it there is no audio at all"},
	"bfe":                          {AdviceKeep, "Base Filtering Engine — enforces firewall and IPsec rules; off disables the firewall and most VPNs"},
	"brokerinfrastructure":         {AdviceKeep, "Background task host for Store apps — powers tiles, notifications and background work; off breaks them"},
	"camsvc":                       {AdviceKeep, "Capability Access Manager — enforces app permissions for camera, mic and location; off breaks privacy-gated access"},
	"cbdhsvc":                      {AdviceKeep, "Clipboard user service — clipboard history (Win+V) and cloud clipboard; off removes them"},
	"cdpsvc":                       {AdviceKeep, "Connected Devices Platform — Nearby Sharing, Phone Link and cross-device handoff; off breaks them"},
	"cdpusersvc":                   {AdviceKeep, "Per-user part of Connected Devices Platform — same Nearby Sharing / cross-device features"},
	"clipsvc":                      {AdviceKeep, "Client License Service — Store app licensing; off can stop Store apps from launching"},
	"coremessagingregistrar":       {AdviceKeep, "CoreMessaging — input/message plumbing of the modern shell; the desktop cannot run without it"},
	"cryptsvc":                     {AdviceKeep, "Cryptographic Services — signature and catalog verification; Windows Update and driver installs depend on it"},
	"cryptsp":                      {AdviceKeep, "Cryptographic support plumbing — part of the certificate/key stack; obscure but system-owned"},
	"dcomlaunch":                   {AdviceKeep, "DCOM Server Process Launcher — starts COM components all over the OS; the system cannot run without it"},
	"deviceassociationservice":     {AdviceKeep, "Device Association Service — pairing and identifying devices; off breaks device pairing"},
	"devicepickerusersvc":          {AdviceKeep, "Device Picker — the 'choose a device' UI used by casting and pairing"},
	"devicenpickupsvc":             {AdviceKeep, "Device pickup plumbing — part of the casting / select-a-device flow"},
	"devicesflowusersvc":           {AdviceKeep, "DevicesFlow — nearby-device connection flows such as casting and continue-on-PC"},
	"dhcp":                         {AdviceKeep, "DHCP Client — obtains your IP address automatically; off means no connectivity on ordinary networks"},
	"dispbrokerdesktopsvc":         {AdviceKeep, "Display policy broker — applies display and brightness policy; off can break display settings"},
	"displayenhancementservice":    {AdviceKeep, "Display Enhancement — HDR and advanced display features; off loses them"},
	"dnscache":                     {AdviceKeep, "DNS Client — resolves and caches domain names; off slows or breaks name resolution"},
	"dps":                          {AdviceKeep, "Diagnostic Policy Service — runs the built-in troubleshooters; off breaks the troubleshooting wizards"},
	"dsrole":                       {AdviceKeep, "DS Role plumbing — relevant to domain controllers; harmless and idle on a client PC"},
	"dusmsvc":                      {AdviceKeep, "Data Usage — meters per-app network usage and metered-connection behavior; off loses usage tracking"},
	"eventlog":                     {AdviceKeep, "Windows Event Log — every component writes events through it; off blinds the system's own logging"},
	"eventsystem":                  {AdviceKeep, "COM+ Event System — publishes system events to subscribers; off breaks event-aware apps"},
	"fontcache":                    {AdviceKeep, "Font Cache — speeds up font loading system-wide; off only slows text rendering"},
	"gpsvc":                        {AdviceKeep, "Group Policy Client — applies domain and local policies; off breaks policy enforcement"},
	"hnetcfg":                      {AdviceKeep, "Home-network configuration helper — legacy network plumbing Windows still owns"},
	"installservice":               {AdviceKeep, "Microsoft Store Install Service — installs and updates Store apps; off breaks both"},
	"keyiso":                       {AdviceKeep, "CNG Key Isolation — guards private keys; off breaks smartcards, EFS and some sign-ins"},
	"lanmanworkstation":            {AdviceKeep, "Workstation — the SMB file-sharing client; off kills network shares and mapped drives"},
	"licensemanager":               {AdviceKeep, "Windows License Manager — licensing checks for Store/Xbox apps; off can break licensed apps"},
	"mpssvc":                       {AdviceKeep, "Windows Defender Firewall — enforces inbound/outbound firewall rules; off leaves the firewall down"},
	"netlogon":                     {AdviceKeep, "Netlogon — the domain authentication channel; off breaks domain sign-in on joined machines"},
	"netman":                       {AdviceKeep, "Network Connections — manages adapters and connection state; off breaks network management"},
	"netprofm":                     {AdviceKeep, "Network List — identifies which network you are on (public/private); off breaks network profiles"},
	"netsetupsvc":                  {AdviceKeep, "Network Setup — joins and configures networks; off breaks joining or reconfiguring them"},
	"networkconnectionbroker":      {AdviceKeep, "Network Connection Broker — grants Store apps network access; off breaks their connectivity"},
	"ngcsvc":                       {AdviceKeep, "Microsoft Passport — Windows Hello PIN/biometric sign-in support; off breaks Hello"},
	"nlasvc":                       {AdviceKeep, "Network Location Awareness — detects the network type for firewall/profile rules; off confuses them"},
	"nsi":                          {AdviceKeep, "Network Store Interface — feeds network info to the system and apps; off breaks network-aware features"},
	"plugplay":                     {AdviceKeep, "Plug and Play — hardware detection and driver loading; the system cannot run without it"},
	"power":                        {AdviceKeep, "Power — enforces power plans and tracks battery state; off breaks power management"},
	"profsvc":                      {AdviceKeep, "User Profile Service — loads your profile and settings at sign-in; off breaks sign-in"},
	"qrssvc":                       {AdviceKeep, "Undocumented Microsoft component — Windows-owned system plumbing; left alone deliberately"},
	"rpceptmapper":                 {AdviceKeep, "RPC Endpoint Mapper — tells programs where services listen; off breaks most inter-process calls"},
	"rpcss":                        {AdviceKeep, "Remote Procedure Call — the IPC backbone Windows is built on; nothing works without it"},
	"samss":                        {AdviceKeep, "Security Accounts Manager — the local account database; off breaks sign-in and account management"},
	"schedule":                     {AdviceKeep, "Task Scheduler — runs every scheduled task on the machine; off stops all of them"},
	"securityhealthservice":        {AdviceKeep, "Windows Security service — feeds the Security dashboard; protection continues but its status UI can go dark"},
	"sens":                         {AdviceKeep, "System Event Notification — notifies apps of logon, network and power events; off breaks event-aware features"},
	"shellhwdetection":             {AdviceKeep, "Shell Hardware Detection — AutoPlay for USB drives and media; off removes the AutoPlay prompts"},
	"smbsvcs":                      {AdviceKeep, "SMB support services — file-sharing plumbing used by the Workstation and Server services"},
	"staterepository":              {AdviceKeep, "State Repository — per-app state store for packaged apps; off breaks Store apps' saved state"},
	"swprv":                        {AdviceKeep, "Software Shadow Copy provider — the engine behind restore points and 'previous versions'"},
	"systemeventsbroker":           {AdviceKeep, "System Events Broker — coordinates events between system components; core plumbing"},
	"themes":                       {AdviceKeep, "Themes — visual styles for the desktop; off reverts Windows to a plain look"},
	"timebroker":                   {AdviceKeep, "Time Broker — coordinates time-related app features; minor, but system-owned"},
	"tokenbroker":                  {AdviceKeep, "Web Account Manager — sign-in tokens for Store apps and Microsoft accounts; off breaks app sign-ins"},
	"trustedinstaller":             {AdviceKeep, "Windows Modules Installer — applies Windows updates and optional components; core servicing"},
	"udkusersvc":                   {AdviceKeep, "User Device Kit — per-user device feature plumbing; minor, system-owned"},
	"usermanager":                  {AdviceKeep, "User Manager — sign-in and run-as plumbing the shell depends on"},
	"usosvc":                       {AdviceKeep, "Update Orchestrator — drives Windows Update scans, installs and reboots; off breaks updating"},
	"uxsms":                        {AdviceKeep, "User experience session plumbing — part of sign-in/session management; system-owned"},
	"vaultsvc":                     {AdviceKeep, "Credential Manager — stores saved passwords and certificates; off breaks credential storage"},
	"vss":                          {AdviceKeep, "Volume Shadow Copy — point-in-time snapshots used by System Restore and many backup tools"},
	"w32time":                      {AdviceKeep, "Windows Time — clock synchronization; off lets the clock drift and can break domain auth"},
	"warpjitsvc":                   {AdviceKeep, "JIT helper used by some graphics and app workloads — minor, system-owned"},
	"wbiosrvc":                     {AdviceKeep, "Windows Biometric Service — fingerprint and face sign-in; off disables biometrics"},
	"wcmsvc":                       {AdviceKeep, "Windows Connection Manager — auto-connects Wi-Fi/cellular and tracks VPN state; off breaks auto-connect"},
	"wdiservicehost":               {AdviceKeep, "Diagnostic Service Host — runs network/system diagnostics in the background"},
	"wdisystemhost":                {AdviceKeep, "Diagnostic System Host — same diagnostics machinery at the system level"},
	"windefend":                    {AdviceKeep, "Microsoft Defender Antivirus — real-time malware protection; off leaves the machine unprotected"},
	"winhttpautoproxysvc":          {AdviceKeep, "WinHTTP proxy auto-discovery — WPAD for services like Windows Update; off breaks their proxy detection"},
	"winmgmt":                      {AdviceKeep, "Windows Management Instrumentation — the system info layer admin tools and drivers query"},
	"wlansvc":                      {AdviceKeep, "WLAN AutoConfig — manages Wi-Fi connections; off means no Wi-Fi"},
	"wmiapsrv":                     {AdviceKeep, "WMI Performance Adapter — serves performance-counter data to monitoring tools"},
	"wpnuserservice":               {AdviceKeep, "Push Notifications (user) — delivers Store-app toast notifications to you"},
	"wpnservice":                   {AdviceKeep, "Push Notifications (system) — the platform side of toast notification delivery"},
	"wscsvc":                       {AdviceKeep, "Security Center — watches antivirus/firewall state and warns you; off silences security warnings"},
	"wsappx":                       {AdviceKeep, "Windows Store service — installs, updates and licenses packaged apps; off breaks Store apps"},
	"wuauserv":                     {AdviceKeep, "Windows Update — downloads and installs updates; off stops updating entirely"},
	"wudfrd":                       {AdviceKeep, "User-Mode Driver Framework — hosts drivers that run outside the kernel; off can break those devices"},
	"appinfo":                      {AdviceKeep, "Application Information — provides UAC elevation; off breaks 'run as administrator'"},
	"appreadiness":                 {AdviceKeep, "App Readiness — prepares packaged apps at sign-in and after updates; off breaks app provisioning"},
	"defragsvc":                    {AdviceKeep, "Optimize Drives — scheduled defrag/TRIM; off loses automatic disk maintenance"},
	"netconnectioncentralaccessor": {AdviceKeep, "Network connection plumbing — undocumented Microsoft component, system-owned"},
	"wrevelstsvc":                  {AdviceKeep, "Windows Recovery Environment event service — obscure recovery plumbing, system-owned"},

	// Windows — safe to disable for most people
	"diagtrack":        {AdviceSafeOff, "Connected User Experiences and Telemetry — Microsoft's usage/diagnostic upload; off loses nothing functional"},
	"dmwappushservice": {AdviceSafeOff, "Device Management push — the MDM channel for enterprise-managed devices; unused on personal PCs"},
	"diagnosticshub.standardcollector.service": {AdviceSafeOff, "Visual Studio diagnostics collector — only runs while a developer traces; useless otherwise"},
	"retsvc":               {AdviceSafeOff, "Recommended Troubleshooting — automatic fix suggestions; off only stops the suggestions"},
	"wercplsupport":        {AdviceSafeOff, "Problem Reports support — feeds the Reliability Monitor / problem-report UI"},
	"wersvc":               {AdviceSafeOff, "Windows Error Reporting — sends crash dumps and reports to Microsoft; off just stops reporting"},
	"stordiag":             {AdviceSafeOff, "Storage diagnostics helper — background disk-health checks; minor"},
	"sgrmbroker":           {AdviceSafeOff, "System Guard broker — Windows self-integrity monitoring, mainly enterprise-relevant"},
	"dosvc":                {AdviceSafeOff, "Delivery Optimization — peer/CDN sharing for update downloads; off just downloads updates conventionally"},
	"mapsbroker":           {AdviceSafeOff, "Downloaded Maps Manager — offline maps for the Maps app; off if you never use it"},
	"fax":                  {AdviceSafeOff, "Fax service — off unless this machine actually sends faxes"},
	"retaildemo":           {AdviceSafeOff, "Retail Demo — store-floor demo-mode plumbing; useless on a personal machine"},
	"lmhosts":              {AdviceSafeOff, "NetBIOS Helper — legacy name resolution; only very old networks still need it"},
	"ssdfsrv":              {AdviceSafeOff, "SSDP Discovery — finds UPnP devices on the LAN (smart TVs, media boxes); off breaks device discovery"},
	"ssdpsrv":              {AdviceSafeOff, "SSDP Discovery — finds UPnP devices on the LAN (smart TVs, media boxes); off breaks device discovery"},
	"upnphost":             {AdviceSafeOff, "UPnP Device Host — lets this PC publish itself to UPnP devices; off if you don't use that"},
	"wcncsvc":              {AdviceSafeOff, "Windows Connect Now — WPS-style simplified Wi-Fi setup; rarely used"},
	"webclient":            {AdviceSafeOff, "WebClient — WebDAV drive support; off if you never mount WebDAV shares"},
	"wecsvc":               {AdviceSafeOff, "Windows Event Collector — pulls event logs from other machines; only for log collectors"},
	"wisvc":                {AdviceSafeOff, "Windows Insider service — preview-build machinery; useless on stable builds"},
	"workfolderssvc":       {AdviceSafeOff, "Work Folders — enterprise file sync; off if your organization doesn't use it"},
	"remoteregistry":       {AdviceSafeOff, "Remote Registry — lets remote admins edit this machine's registry; off is also the safer default"},
	"winrm":                {AdviceSafeOff, "Windows Remote Management — PowerShell remoting; off if nobody remote-manages this PC"},
	"peerdistsvc":          {AdviceSafeOff, "BranchCache — caches shared files across a LAN; an enterprise-only feature"},
	"pnrpauto":             {AdviceSafeOff, "Peer Name Resolution — auto config for legacy P2P apps; rarely used"},
	"pnrpsvc":              {AdviceSafeOff, "Peer Name Resolution — the same legacy P2P resolution protocol"},
	"p2pimsvc":             {AdviceSafeOff, "Peer Networking identity — identity service for legacy P2P apps; rarely used"},
	"p2psvc":               {AdviceSafeOff, "Peer Networking — grouping service for legacy P2P apps; rarely used"},
	"axinstsv":             {AdviceSafeOff, "ActiveX Installer — installs signed ActiveX controls; only legacy intranet sites need it"},
	"browser":              {AdviceSafeOff, "Computer Browser — the old network-neighborhood list; superseded long ago"},
	"napagent":             {AdviceSafeOff, "Network Access Protection — enterprise health-check agent; off on unmanaged PCs"},
	"nettcpportsharing":    {AdviceSafeOff, "Net.Tcp Port Sharing — lets WCF services share a TCP port; developer/enterprise only"},
	"snmptrap":             {AdviceSafeOff, "SNMP Trap — receives SNMP alerts; only monitoring setups use it"},
	"spatialdata":          {AdviceSafeOff, "Spatial Data — spatial anchors for Mixed Reality; off if you don't use VR/MR"},
	"tapisrv":              {AdviceSafeOff, "Telephony — the old TAPI phone/modem API; off on modern machines"},
	"wephostsvc":           {AdviceSafeOff, "Wireless extensibility host — enterprise Wi-Fi scenario plumbing; minor"},
	"whesvc":               {AdviceSafeOff, "Obscure Microsoft component — Windows-owned; safely ignored on a personal PC"},
	"wpcmonsvc":            {AdviceSafeOff, "Parental Controls monitor — enforces Family Safety limits; off if you don't use it"},
	"wpcsvc":               {AdviceSafeOff, "Parental Controls — the older family-safety service; same answer"},
	"homegroupprovider":    {AdviceSafeOff, "HomeGroup provider — the retired Windows 7-era sharing feature; nothing uses it now"},
	"homegrouplistener":    {AdviceSafeOff, "HomeGroup listener — same retired feature"},
	"wbengine":             {AdviceSafeOff, "Windows Backup engine — the legacy 'Backup and Restore' feature; off if you don't use it"},
	"sdrsvc":               {AdviceSafeOff, "Windows Backup scheduler — schedules the same legacy backups"},
	"dssvc":                {AdviceSafeOff, "Data Sharing Service — brokers data between apps; very few apps use it"},
	"davauthservice":       {AdviceSafeOff, "Wired AutoConfig — 802.1X wired authentication; corporate LANs only"},
	"cscservice":           {AdviceSafeOff, "Offline Files — caches network shares for offline use; off if you don't use it"},
	"wmpnetworksvc":        {AdviceSafeOff, "Windows Media Player Network Sharing — serves your media library to the LAN; off if you don't stream it"},
	"sshd":                 {AdviceSafeOff, "OpenSSH Server — lets you SSH into this machine; off if you never remote in"},
	"perceptionsimulation": {AdviceSafeOff, "Perception Simulation — Mixed Reality simulation for developers"},
	"mixedrealityopenxr":   {AdviceSafeOff, "Mixed Reality OpenXR — the Windows VR runtime; off if you don't run VR"},
	"nosxp":                {AdviceSafeOff, "Obscure Microsoft component — Windows-owned; safely ignored on a personal PC"},
	"spectrum":             {AdviceSafeOff, "Spectrum — sensor/spectrum service plumbing; minor, rarely load-bearing"},
	"phonesvc":             {AdviceSafeOff, "Phone service — backend for Phone Link features; off if you don't use the phone companion"},
	"pushtoinstall":        {AdviceSafeOff, "Push to Install — pushes Store app installs to this PC remotely; off if unused"},
	"xboxgipsvc":           {AdviceSafeOff, "Xbox Accessory Management — manages Xbox controllers/accessories; off if you don't game"},
	"xblgamesave":          {AdviceSafeOff, "Xbox Live Game Save — cloud saves for Store games; off loses save syncing"},
	"xboxnetapisvc":        {AdviceSafeOff, "Xbox Live Networking — multiplayer plumbing for Store games; off if you don't play them"},
	"wephostsvcs":          {AdviceSafeOff, "Wireless extensibility host — enterprise Wi-Fi scenario plumbing; minor"},

	// Windows — optional, think first
	"wsearch":               {AdviceOptional, "Windows Search — builds the index behind Start/Explorer search; off makes searches crawl files the slow way"},
	"sysmain":               {AdviceOptional, "SysMain (ex-Superfetch) — preloads frequently used apps; off only makes first launches slower"},
	"spooler":               {AdviceOptional, "Print Spooler — queues print jobs; off means this machine cannot print at all"},
	"printnotify":           {AdviceOptional, "Print Notifications — printer status alerts; off just silences them"},
	"iphlpsvc":              {AdviceOptional, "IP Helper — IPv6 transition tunnels (6to4, Teredo, ISATAP); most home users never need it"},
	"lanmanserver":          {AdviceOptional, "Server — shares YOUR files and printers to the network; off stops others reaching your shares"},
	"sharedaccess":          {AdviceOptional, "Internet Connection Sharing — shares this PC's internet to other devices; off if you don't"},
	"icssvc":                {AdviceOptional, "Cellular/hotspot helper — backs the mobile-hotspot feature; off if you never share internet"},
	"dot3svc":               {AdviceOptional, "Wired AutoConfig — 802.1X wired authentication; corporate LANs only"},
	"seclogon":              {AdviceOptional, "Secondary Logon — 'Run as different user'; off breaks that feature"},
	"ikeext":                {AdviceOptional, "IKE/AuthIP — IPsec key exchange; off breaks IPsec VPNs"},
	"policyagent":           {AdviceOptional, "IPsec Policy Agent — applies IPsec encryption policy; off breaks IPsec VPNs/rules"},
	"rasauto":               {AdviceOptional, "Remote Access Auto Connection — dials VPN/PPP links on demand; off if you don't use them"},
	"rasman":                {AdviceOptional, "Remote Access Connection Manager — the VPN/dial-up engine; off breaks VPN connections"},
	"remoteaccess":          {AdviceOptional, "Routing and Remote Access — turns the PC into a router/VPN server; almost always safe off"},
	"remoteaccessservice":   {AdviceOptional, "Legacy remote-access plumbing — DirectAccess-era machinery; rarely used"},
	"termservice":           {AdviceOptional, "Remote Desktop Services — lets others RDP into this machine; off blocks incoming RDP"},
	"umrdpservice":          {AdviceOptional, "RDP redirector — device/port redirection inside remote sessions; off affects only RDP use"},
	"sessionenv":            {AdviceOptional, "Remote Desktop Configuration — prepares the remote session environment"},
	"pcasvc":                {AdviceOptional, "Program Compatibility Assistant — detects old-app issues and suggests fixes; off just stops the nags"},
	"wlidsvc":               {AdviceOptional, "Microsoft Account Sign-in Assistant — MSA sign-in for apps; off can break Store/MSA sign-ins"},
	"efssvc":                {AdviceOptional, "Encrypting File System — NTFS file/folder encryption; off if you never encrypt files"},
	"fdphost":               {AdviceOptional, "Function Discovery Provider — publishes this PC to the network; part of network discovery"},
	"fdrespub":              {AdviceOptional, "Function Discovery Resource — advertises your shared resources; same network-discovery machinery"},
	"scardsvr":              {AdviceOptional, "Smart Card — smartcard sign-in and certificates; off if you don't use smartcards"},
	"scdeviceenum":          {AdviceOptional, "Smart Card Enumeration — detects smartcard readers; same answer"},
	"certpropsvc":           {AdviceOptional, "Certificate Propagation — exposes smartcard certificates to apps"},
	"tabletinputservice":    {AdviceOptional, "Touch Keyboard/Handwriting — pen and on-screen keyboard input; off kills the touch keyboard"},
	"stisvc":                {AdviceOptional, "Windows Image Acquisition — scanners and cameras; off breaks scanning"},
	"sensordataservice":     {AdviceOptional, "Sensor Data service — aggregates sensor readings; minor"},
	"sensorservice":         {AdviceOptional, "Sensor Service — manages brightness/orientation sensors"},
	"sensrsvc":              {AdviceOptional, "Sensor Monitoring — adaptive brightness and auto-rotation; off stops auto-adjustments"},
	"naturalauthentication": {AdviceOptional, "Natural Authentication — signal-based features like Dynamic Lock; minor"},
	"xblauthmanager":        {AdviceOptional, "Xbox Live Auth Manager — sign-in for Xbox/Store games; off breaks Xbox sign-in"},
	"trkwks":                {AdviceOptional, "Distributed Link Tracking — keeps shortcuts working across file moves on NTFS; minor"},
	"wmiacpi":               {AdviceCaution, "WMI-to-ACPI bridge — reads firmware thermal/power tables; off can break vendor monitoring tools"},
	"waledigest":            {AdviceCaution, "Obscure Windows authentication component — system-owned; leave it alone"},
	"wdisvc":                {AdviceCaution, "Windows Deployment/Imaging plumbing — servicing machinery; leave it alone"},

	// OEM / driver companions
	"igfxcuiservice2.0.0.0":          {AdviceCaution, rDriver},
	"igccservice":                    {AdviceCaution, rDriver},
	"igccpresservice":                {AdviceOptional, rCosmetic},
	"igdlpsvc":                       {AdviceCaution, rDriver},
	"cphs":                           {AdviceCaution, rDriver},
	"esifsvc":                        {AdviceCaution, rDriver},
	"dptftchesvc":                    {AdviceCaution, rDriver},
	"intelpmt":                       {AdviceCaution, rTelemetry},
	"intelpps":                       {AdviceCaution, rDriver},
	"intelepheleventservice":         {AdviceSafeOff, rTelemetry},
	"intelepheservice":               {AdviceSafeOff, rTelemetry},
	"ipisv":                          {AdviceSafeOff, rTelemetry},
	"dsaservice":                     {AdviceSafeOff, rUpdater},
	"dsauploadservice":               {AdviceSafeOff, rTelemetry},
	"nvdisplay.containerlocalsystem": {AdviceCaution, rDriver},
	"nvtelemetrycontainer":           {AdviceSafeOff, rTelemetry},
	"amd crash defender service":     {AdviceCaution, rDriver},
	"amd log utility":                {AdviceSafeOff, rTelemetry},
	"amdaeupdate":                    {AdviceSafeOff, rUpdater},
	"rtkaudioservice":                {AdviceCaution, rDriver},
	"rtkhdaservice":                  {AdviceCaution, rDriver},
	"rtkvadservice":                  {AdviceCaution, rDriver},
	"rthdsvc":                        {AdviceCaution, rDriver},
	"rtkunicontroluniversal":         {AdviceOptional, rUnused},
	"dolbydaxapiservice":             {AdviceCaution, rDriver},
	"dax3api":                        {AdviceCaution, rDriver},
	"synapteservice":                 {AdviceCaution, rDriver},
	"conexant audio service":         {AdviceCaution, rDriver},
	"hdrxaudioservice":               {AdviceCaution, rDriver},

	// Virtualization / containers / servers
	"vmicheartbeatservice":             {AdviceCaution, rVM},
	"vmicshutdown":                     {AdviceCaution, rVM},
	"vmictimesync":                     {AdviceCaution, rVM},
	"vmicvss":                          {AdviceCaution, rVM},
	"vmickvpexchange":                  {AdviceCaution, rVM},
	"vmicguestinterfaceservice":        {AdviceCaution, rVM},
	"vmicrdv":                          {AdviceCaution, rVM},
	"vmicsessionmapper":                {AdviceCaution, rVM},
	"vmicvmsession":                    {AdviceCaution, rVM},
	"vmcompute":                        {AdviceCaution, rVM},
	"vmms":                             {AdviceCaution, rVM},
	"lxssmanager":                      {AdviceOptional, rVM},
	"wslservice":                       {AdviceOptional, dWslSvc},
	"com.docker.service":               {AdviceOptional, dDocker},
	"postgresql":                       {AdviceOptional, rServer},
	"mysql":                            {AdviceOptional, rServer},
	"mongodb":                          {AdviceOptional, rServer},
	"redis":                            {AdviceOptional, rServer},
	"nginx":                            {AdviceOptional, rServer},
	"apache2.4":                        {AdviceOptional, rServer},
	"tailscaled":                       {AdviceOptional, rProxy},
	"codexsandboxservice.openai.codex": {AdviceOptional, dCodexSvc},
	"clicktorunsvc":                    {AdviceKeep, dClickToRun},
	"clash_verge_service":              {AdviceOptional, dClashSvc},
	"gameviewerservice":                {AdviceOptional, dGameViewer},
	"intelgraphicssoftwareservice":     {AdviceOptional, dIntelGfx},
	"lenovopcmanagerservice":           {AdviceOptional, dLenovoPCM},
	"pcmanager service store":          {AdviceSafeOff, dMsPCM},
	"osppsvc":                          {AdviceKeep, dOspp},
	"ose":                              {AdviceOptional, dOse},
	"officesvccomhandler":              {AdviceOptional, dOfficeSvc},
	"coworkvmservice":                  {AdviceOptional, "Claude's cowork VM service — off if you don't use Claude's VM features"},

	// Common third-party services
	"gupdate":                      {AdviceSafeOff, dBrowserUpd},
	"gupdatem":                     {AdviceSafeOff, dBrowserUpd},
	"googleupdaterinternalservice": {AdviceSafeOff, dBrowserUpd},
	"googleupdaterservice":         {AdviceSafeOff, dBrowserUpd},
	"adobeupdateservice":           {AdviceSafeOff, rUpdater},
	"adobeagsservice":              {AdviceSafeOff, rUpdater},
	"adobearmservice":              {AdviceSafeOff, rUpdater},
	"adobegcclient":                {AdviceSafeOff, rUpdater},
	"ccxprocess":                   {AdviceSafeOff, rHelper},
	"ccmservice":                   {AdviceSafeOff, rHelper},
	"bonjour":                      {AdviceOptional, rOnlyIf},
	"applemobiledeviceservice":     {AdviceOptional, rOnlyIf},
	"steamservice":                 {AdviceOptional, rGame},
	"epicgameslauncher":            {AdviceSafeOff, rGame},
	"epic online services":         {AdviceSafeOff, rGame},
	"npcap":                        {AdviceKeep, "Packet capture driver support — Wireshark/Nmap need it"},
}

// adviceApps pattern-matches app/task entries: the needle is searched in
// the lowercased name+exe+command+publisher. First hit wins — order
// specific before generic.
var adviceApps = []struct {
	match string
	adv   Advice
}{
	// specific widely-installed items — these precede the generic
	// classes so the detailed text wins on real machines
	{"securityhealth", Advice{AdviceOptional, dSecHealth}},
	{"rtkaudu", Advice{AdviceCaution, dRtk}},
	{"realtek", Advice{AdviceCaution, dRtk}},
	{"logilda", Advice{AdviceSafeOff, dLogiUpd}},
	{"download assistant", Advice{AdviceSafeOff, dUpdAssist}},
	{"edgeautolaunch", Advice{AdviceSafeOff, dEdgePreload}},
	{"win-session-start", Advice{AdviceSafeOff, dEdgePreload}},
	{"collabsync", Advice{AdviceSafeOff, dCollabSync}},
	{"acrobat synchronizer", Advice{AdviceSafeOff, dCollabSync}},
	{"magpie", Advice{AdviceOptional, dMagpie}},
	{"warp", Advice{AdviceSafeOff, dWarp}},
	{"gameviewer", Advice{AdviceOptional, dGameViewer}},
	{"tabbit", Advice{AdviceSafeOff, dAppUpd}},
	{"doubao", Advice{AdviceSafeOff, dAppUpd}},
	{"quarkupdater", Advice{AdviceSafeOff, dQuarkUpd}},
	{"edgeupdate", Advice{AdviceSafeOff, dBrowserUpd}},
	{"googleupdate", Advice{AdviceSafeOff, dBrowserUpd}},
	{"lenovopcmanager", Advice{AdviceOptional, dLenovoPCM}},
	{"power manager", Advice{AdviceOptional, dLenovoPM}},
	{"mspcmanager", Advice{AdviceSafeOff, dMsPCM}},
	{"pc manager", Advice{AdviceSafeOff, dPCMgr}},
	{"pcmanager", Advice{AdviceSafeOff, dPCMgr}},
	{"电脑管家", Advice{AdviceSafeOff, dPCMgr}},
	{"intelgraphics", Advice{AdviceOptional, dIntelGfx}},
	{"intel arc", Advice{AdviceOptional, dIntelGfx}},
	{"intelarcsoftware", Advice{AdviceOptional, dIntelGfx}},
	{"synaptics", Advice{AdviceOptional, dSynaptics}},
	{"synwinui", Advice{AdviceOptional, dSynaptics}},
	{"touchpad", Advice{AdviceOptional, dSynaptics}},
	{"startexperiencesapp", Advice{AdviceSafeOff, dStartFeed}},
	{"startfeed", Advice{AdviceSafeOff, dStartFeed}},
	{"microsoft.todos", Advice{AdviceSafeOff, dToDo}},
	{"to do", Advice{AdviceSafeOff, dToDo}},
	{"windowsterminal", Advice{AdviceSafeOff, dTerminal}},
	{"startterminal", Advice{AdviceSafeOff, dTerminal}},
	{"powerautomate", Advice{AdviceOptional, dPowerAuto}},
	{"power automate", Advice{AdviceOptional, dPowerAuto}},
	{"office automatic updates", Advice{AdviceSafeOff, dOfficeUpd}},
	{"office feature updates", Advice{AdviceSafeOff, dOfficeUpd}},
	{"officec2rclient", Advice{AdviceSafeOff, dOfficeUpd}},
	{"onnotem", Advice{AdviceOptional, dOneNoteTool}},
	{"send to onenote", Advice{AdviceOptional, dOneNoteTool}},
	{"office telemetry", Advice{AdviceSafeOff, dOfficeTel}},
	{"officetelemetry", Advice{AdviceSafeOff, dOfficeTel}},
	{"telemetry agent", Advice{AdviceSafeOff, dOfficeTel}},
	{"msoia", Advice{AdviceSafeOff, dOfficeTel}},
	{"officebackgroundtaskhandler", Advice{AdviceOptional, dOfficeBG}},
	{"officeclicktorun", Advice{AdviceKeep, dClickToRun}},
	{"clicktorun", Advice{AdviceKeep, dClickToRun}},
	{"skype for business", Advice{AdviceOptional, dLync}},
	{"lync", Advice{AdviceOptional, dLync}},
	{"groovemonitor", Advice{AdviceSafeOff, dGroove}},
	{"onedrive per-machine", Advice{AdviceSafeOff, dOneDriveUpd}},
	{"onedrive reporting", Advice{AdviceSafeOff, dOneDriveUpd}},
	{"onedrive startup task", Advice{AdviceSafeOff, dOneDriveUpd}},

	// built-in Windows scheduled tasks — everyone has these
	{"scheduleddefrag", Advice{AdviceOptional, "Optimize Drives — the weekly defrag/TRIM task; off loses automatic disk maintenance"}},
	{"memory diagnostic", Advice{AdviceOptional, "Memory Diagnostic — schedules a RAM check after crashes; only runs when triggered"}},
	{"runfullmemory", Advice{AdviceOptional, "Memory Diagnostic — schedules a RAM check after crashes; only runs when triggered"}},
	{"customer experience", Advice{AdviceSafeOff, "Customer Experience Improvement — Microsoft's telemetry tasks; off loses nothing functional"}},
	{"ceip", Advice{AdviceSafeOff, "Customer Experience Improvement — Microsoft's telemetry tasks; off loses nothing functional"}},
	{"sihboot", Advice{AdviceCaution, "Windows Update bootstrapper — revives the update client; off can stall Windows Update"}},
	{"sihclient", Advice{AdviceCaution, "Windows Update bootstrapper — revives the update client; off can stall Windows Update"}},
	{"synchronize time", Advice{AdviceOptional, "Time sync task — keeps the clock aligned; off lets it drift"}},
	{"time zone", Advice{AdviceOptional, "Time zone data refresh — keeps timezone definitions current; minor"}},
	{"bitlocker", Advice{AdviceCaution, "BitLocker maintenance — housekeeping for encrypted drives; don't disable on encrypted systems"}},
	{"mpdefender", Advice{AdviceKeep, "Defender scheduled scan — your periodic malware scan; keep it"}},
	{"windows defender", Advice{AdviceKeep, "Defender scheduled scan — your periodic malware scan; keep it"}},
	{"silentcleanup", Advice{AdviceOptional, "Storage maintenance — auto-cleans temp files; off just lets them accumulate"}},
	{"diskcleanup", Advice{AdviceOptional, "Storage maintenance — auto-cleans temp files; off just lets them accumulate"}},
	{"diskfootprint", Advice{AdviceOptional, "Storage maintenance — auto-cleans temp files; off just lets them accumulate"}},
	{"ngentask", Advice{AdviceOptional, ".NET NGEN — precompiles .NET assemblies for faster app startup; off just slows .NET launches"}},
	{".net framework ngen", Advice{AdviceOptional, ".NET NGEN — precompiles .NET assemblies for faster app startup; off just slows .NET launches"}},

	// input methods first — safe to keep
	{"sogou", Advice{AdviceKeep, rInput}},
	{"sogouinput", Advice{AdviceKeep, rInput}},
	{"baiduinput", Advice{AdviceKeep, rInput}},
	{"qqpinyin", Advice{AdviceKeep, rInput}},
	{"输入法", Advice{AdviceKeep, rInput}},
	{"pinyin", Advice{AdviceKeep, rInput}},

	// security suites
	{"360safe", Advice{AdviceKeep, rSecurity}},
	{"360tray", Advice{AdviceKeep, rSecurity}},
	{"zhudongfangyu", Advice{AdviceKeep, rSecurity}},
	{"360leakfixer", Advice{AdviceKeep, rSecurity}},
	{"huorong", Advice{AdviceKeep, rSecurity}},
	{"hipstray", Advice{AdviceKeep, rSecurity}},
	{"usysdiag", Advice{AdviceKeep, rSecurity}},
	{"qqprotect", Advice{AdviceKeep, rSecurity}},
	{"nod32", Advice{AdviceKeep, rSecurity}},
	{"eset", Advice{AdviceKeep, rSecurity}},
	{"avast", Advice{AdviceKeep, rSecurity}},
	{"kaspersky", Advice{AdviceKeep, rSecurity}},
	{"mcafee", Advice{AdviceKeep, rSecurity}},
	{"norton", Advice{AdviceKeep, rSecurity}},
	{"bitdefender", Advice{AdviceKeep, rSecurity}},
	{"trendmicro", Advice{AdviceKeep, rSecurity}},
	{"malwarebytes", Advice{AdviceKeep, rSecurity}},

	// backup agents — off stops backups
	{"veeam", Advice{AdviceKeep, rBackup}},
	{"macrium", Advice{AdviceKeep, rBackup}},
	{"acronis", Advice{AdviceKeep, rBackup}},
	{"carbonite", Advice{AdviceKeep, rBackup}},
	{"crashplan", Advice{AdviceKeep, rBackup}},
	{"syncback", Advice{AdviceKeep, rBackup}},
	{"duplicati", Advice{AdviceKeep, rBackup}},

	// VPN / proxy / remote access
	{"clash", Advice{AdviceOptional, dClashApp}},
	{"mihomo", Advice{AdviceOptional, rProxy}},
	{"v2ray", Advice{AdviceOptional, rProxy}},
	{"nekoray", Advice{AdviceOptional, rProxy}},
	{"sing-box", Advice{AdviceOptional, rProxy}},
	{"singbox", Advice{AdviceOptional, rProxy}},
	{"wireguard", Advice{AdviceOptional, rProxy}},
	{"openvpn", Advice{AdviceOptional, rProxy}},
	{"tailscale", Advice{AdviceOptional, dTailscale}},
	{"zerotier", Advice{AdviceOptional, rProxy}},
	{"protonvpn", Advice{AdviceOptional, rProxy}},
	{"nordvpn", Advice{AdviceOptional, rProxy}},
	{"expressvpn", Advice{AdviceOptional, rProxy}},
	{"surfshark", Advice{AdviceOptional, rProxy}},
	{"mullvad", Advice{AdviceOptional, rProxy}},
	{"windscribe", Advice{AdviceOptional, rProxy}},
	{"sunlogin", Advice{AdviceOptional, rRemote}},
	{"oray", Advice{AdviceOptional, rRemote}},
	{"todesk", Advice{AdviceOptional, rRemote}},
	{"rustdesk", Advice{AdviceOptional, rRemote}},
	{"teamviewer", Advice{AdviceOptional, rRemote}},
	{"anydesk", Advice{AdviceOptional, rRemote}},
	{"parsec", Advice{AdviceOptional, rRemote}},
	{"nomachine", Advice{AdviceOptional, rRemote}},
	{"splashtop", Advice{AdviceOptional, rRemote}},
	{"realvnc", Advice{AdviceOptional, rRemote}},
	{"chrome remote", Advice{AdviceOptional, rRemote}},

	// cloud sync
	{"onedrive", Advice{AdviceOptional, dOneDrive}},
	{"dropbox", Advice{AdviceOptional, rSync}},
	{"googledrive", Advice{AdviceOptional, rSync}},
	{"baidunetdisk", Advice{AdviceOptional, rSync}},
	{"aliyunpan", Advice{AdviceOptional, rSync}},
	{"aliyundrive", Advice{AdviceOptional, rSync}},
	{"quark", Advice{AdviceOptional, rSync}},
	{"nextcloud", Advice{AdviceOptional, rSync}},
	{"seafile", Advice{AdviceOptional, rSync}},
	{"syncthing", Advice{AdviceOptional, rSync}},
	{"resilio", Advice{AdviceOptional, rSync}},
	{"icloud", Advice{AdviceOptional, rSync}},
	{"wpscloudsvr", Advice{AdviceOptional, rSync}},

	// messaging / meetings
	{"wechat", Advice{AdviceOptional, rIM}},
	{"wxwork", Advice{AdviceOptional, rIM}},
	{"wecom", Advice{AdviceOptional, rIM}},
	{"qq.exe", Advice{AdviceOptional, rIM}},
	{"tencent tim", Advice{AdviceOptional, rIM}},
	{"dingtalk", Advice{AdviceOptional, dDingTalk}},
	{"feishu", Advice{AdviceOptional, rIM}},
	{"lark", Advice{AdviceOptional, rIM}},
	{"wemeet", Advice{AdviceOptional, rIM}},
	{"welink", Advice{AdviceOptional, rIM}},
	{"teams", Advice{AdviceOptional, rIM}},
	{"slack", Advice{AdviceOptional, rIM}},
	{"discord", Advice{AdviceSafeOff, rIM}},
	{"telegram", Advice{AdviceOptional, rIM}},
	{"whatsapp", Advice{AdviceOptional, rIM}},
	{"signal", Advice{AdviceOptional, rIM}},
	{"skype", Advice{AdviceOptional, rIM}},
	{"zoom", Advice{AdviceSafeOff, rOnDemand}},
	{"thunderbird", Advice{AdviceOptional, rMailOff}},
	{"outlook", Advice{AdviceOptional, dOutlook}},

	// game platforms
	{"steam", Advice{AdviceSafeOff, rGame}},
	{"epicgames", Advice{AdviceSafeOff, rGame}},
	{"epic games", Advice{AdviceSafeOff, rGame}},
	{"gog", Advice{AdviceSafeOff, rGame}},
	{"eabackground", Advice{AdviceSafeOff, rGame}},
	{"origin", Advice{AdviceSafeOff, rGame}},
	{"battle.net", Advice{AdviceSafeOff, rGame}},
	{"battlenet", Advice{AdviceSafeOff, rGame}},
	{"ubisoft", Advice{AdviceSafeOff, rGame}},
	{"uplay", Advice{AdviceSafeOff, rGame}},
	{"wegame", Advice{AdviceSafeOff, rGame}},
	{"riot", Advice{AdviceSafeOff, rGame}},
	{"hoyoplay", Advice{AdviceSafeOff, rGame}},
	{"mihoyo", Advice{AdviceSafeOff, rGame}},
	{"xbox", Advice{AdviceSafeOff, dXbox}},

	// media & downloaders
	{"spotify", Advice{AdviceSafeOff, rHelper}},
	{"cloudmusic", Advice{AdviceSafeOff, rHelper}},
	{"qqmusic", Advice{AdviceSafeOff, rHelper}},
	{"kugou", Advice{AdviceSafeOff, rHelper}},
	{"kuwo", Advice{AdviceSafeOff, rHelper}},
	{"bilibili", Advice{AdviceSafeOff, rHelper}},
	{"iqiyi", Advice{AdviceSafeOff, rHelper}},
	{"youku", Advice{AdviceSafeOff, rHelper}},
	{"tencentvideo", Advice{AdviceSafeOff, rHelper}},
	{"vlc", Advice{AdviceSafeOff, rHelper}},
	{"thunder", Advice{AdviceSafeOff, rOnDemand}},
	{"xunlei", Advice{AdviceSafeOff, rOnDemand}},
	{"qbittorrent", Advice{AdviceOptional, rOnDemand}},
	{"utorrent", Advice{AdviceOptional, rOnDemand}},
	{"transmission", Advice{AdviceOptional, rOnDemand}},
	{"motrix", Advice{AdviceOptional, rOnDemand}},
	{"idm", Advice{AdviceOptional, rOnDemand}},
	{"freedownloadmanager", Advice{AdviceOptional, rOnDemand}},

	// updaters & preloaders
	{"update", Advice{AdviceSafeOff, rUpdater}},
	{"updater", Advice{AdviceSafeOff, rUpdater}},
	{"adobe arm", Advice{AdviceSafeOff, rUpdater}},
	{"adobearm", Advice{AdviceSafeOff, rUpdater}},
	{"adobegc", Advice{AdviceSafeOff, rUpdater}},
	{"adobe creative cloud", Advice{AdviceSafeOff, rHelper}},
	{"ccxprocess", Advice{AdviceSafeOff, rHelper}},
	{"acrotray", Advice{AdviceSafeOff, rCosmetic}},
	{"foxit", Advice{AdviceSafeOff, rUpdater}},
	{"iobit", Advice{AdviceSafeOff, rUpdater}},
	{"driverbooster", Advice{AdviceSafeOff, rUpdater}},
	{"driver easy", Advice{AdviceSafeOff, rUpdater}},
	{"microsoft edge", Advice{AdviceSafeOff, rPreload}},
	{"edge", Advice{AdviceSafeOff, rPreload}},
	{"chrome", Advice{AdviceSafeOff, rPreload}},
	{"firefox", Advice{AdviceSafeOff, rPreload}},
	{"brave", Advice{AdviceSafeOff, rPreload}},
	{"opera", Advice{AdviceSafeOff, rPreload}},
	{"qqbrowser", Advice{AdviceSafeOff, rPreload}},
	{"360chrome", Advice{AdviceSafeOff, rPreload}},
	{"2345", Advice{AdviceSafeOff, rJunkSoft}},
	{"hao123", Advice{AdviceSafeOff, rJunkSoft}},

	// vendor trays & tuning
	{"nvcpl", Advice{AdviceOptional, rCosmetic}},
	{"nvidia share", Advice{AdviceOptional, rUnused}},
	{"nvtray", Advice{AdviceOptional, rCosmetic}},
	{"igfxtray", Advice{AdviceOptional, rCosmetic}},
	{"igfxpers", Advice{AdviceOptional, rCosmetic}},
	{"igfxhk", Advice{AdviceOptional, rUnused}},
	{"hkcmd", Advice{AdviceOptional, rCosmetic}},
	{"rthdvcpl", Advice{AdviceOptional, rUnused}},
	{"razer", Advice{AdviceOptional, rUnused}},
	{"lghub", Advice{AdviceOptional, rUnused}},
	{"logitech", Advice{AdviceOptional, rUnused}},
	{"icue", Advice{AdviceOptional, rUnused}},
	{"openrgb", Advice{AdviceOptional, rUnused}},
	{"signalrgb", Advice{AdviceOptional, rUnused}},
	{"armourycrate", Advice{AdviceOptional, rCompanion}},
	{"armoury crate", Advice{AdviceOptional, rCompanion}},
	{"msiafterburner", Advice{AdviceOptional, rUnused}},
	{"rtss", Advice{AdviceOptional, rUnused}},
	{"evga precision", Advice{AdviceOptional, rUnused}},
	{"fancontrol", Advice{AdviceOptional, rUnused}},
	{"argus monitor", Advice{AdviceOptional, rUnused}},
	{"speedfan", Advice{AdviceOptional, rUnused}},
	{"asus framework", Advice{AdviceCaution, rDriver}},
	{"lenovo vantage", Advice{AdviceOptional, rCompanion}},
	{"hp support", Advice{AdviceOptional, rCompanion}},
	{"dell", Advice{AdviceOptional, rCompanion}},
	{"mysupport", Advice{AdviceOptional, rCompanion}},
	{"crystaldiskinfo", Advice{AdviceOptional, rUnused}},
	{"hwinfo", Advice{AdviceOptional, rUnused}},
	{"aida64", Advice{AdviceOptional, rUnused}},
	{"openhardwaremonitor", Advice{AdviceOptional, rUnused}},

	// desktop customization
	{"wallpaper engine", Advice{AdviceOptional, rUnused}},
	{"translucenttb", Advice{AdviceOptional, rCustomUI}},
	{"startisback", Advice{AdviceOptional, rCustomUI}},
	{"startallback", Advice{AdviceOptional, rCustomUI}},
	{"open shell", Advice{AdviceOptional, rCustomUI}},
	{"start11", Advice{AdviceOptional, rCustomUI}},
	{"fences", Advice{AdviceOptional, rCustomUI}},
	{"stardock", Advice{AdviceOptional, rCustomUI}},
	{"rainmeter", Advice{AdviceOptional, rCustomUI}},
	{"powertoys", Advice{AdviceOptional, rUnused}},
	{"fancyzones", Advice{AdviceOptional, rCustomUI}},
	{"eartrumpet", Advice{AdviceOptional, rCosmetic}},
	{"modernflyouts", Advice{AdviceOptional, rCustomUI}},

	// productivity utilities
	{"snipaste", Advice{AdviceOptional, dSnipaste}},
	{"sharex", Advice{AdviceOptional, rUnused}},
	{"everything", Advice{AdviceOptional, rUnused}},
	{"listary", Advice{AdviceOptional, rUnused}},
	{"utools", Advice{AdviceOptional, rUnused}},
	{"ditto", Advice{AdviceOptional, rUnused}},
	{"autohotkey", Advice{AdviceOptional, rUnused}},
	{"grammary", Advice{AdviceOptional, rHelper}},
	{"grammarly", Advice{AdviceOptional, rUnused}},
	{"deepl", Advice{AdviceOptional, rUnused}},
	{"notion", Advice{AdviceOptional, rHelper}},
	{"obsidian", Advice{AdviceOptional, rHelper}},
	{"todoist", Advice{AdviceOptional, rHelper}},
	{"ticktick", Advice{AdviceOptional, rHelper}},
	{"evernote", Advice{AdviceOptional, rHelper}},
	{"onenote", Advice{AdviceOptional, dOneNoteTool}},
	{"1password", Advice{AdviceOptional, rUnused}},
	{"bitwarden", Advice{AdviceOptional, rUnused}},
	{"keepass", Advice{AdviceOptional, rUnused}},

	// virtualization & dev
	{"docker desktop", Advice{AdviceOptional, dDocker}},
	{"rancher desktop", Advice{AdviceOptional, rVM}},
	{"virtualbox", Advice{AdviceOptional, rVM}},
	{"vmware", Advice{AdviceOptional, rVM}},
	{"wsl", Advice{AdviceOptional, rVM}},
	{"jetbrains toolbox", Advice{AdviceOptional, rUnused}},
	{"postman", Advice{AdviceOptional, rHelper}},
	{"github desktop", Advice{AdviceOptional, rHelper}},
	{"gitkraken", Advice{AdviceOptional, rHelper}},
	{"jenkins", Advice{AdviceOptional, rServer}},
	{"plex", Advice{AdviceOptional, rServer}},
	{"jellyfin", Advice{AdviceOptional, rServer}},
	{"emby", Advice{AdviceOptional, rServer}},

	// ai & misc helpers
	{"chatgpt", Advice{AdviceSafeOff, dChatGPT}},
	{"openai", Advice{AdviceSafeOff, dChatGPT}},
	{"claude", Advice{AdviceSafeOff, rHelper}},
	{"gemini", Advice{AdviceSafeOff, rHelper}},
	{"deepseek", Advice{AdviceSafeOff, rHelper}},
	{"copilot", Advice{AdviceOptional, rHelper}},
	{"cortana", Advice{AdviceSafeOff, rHelper}},
	{"yourphone", Advice{AdviceOptional, dYourPhone}},
	{"phoneexperiencehost", Advice{AdviceOptional, rOnlyIf}},
	{"itunes", Advice{AdviceOptional, rOnlyIf}},
	{"voicemeeter", Advice{AdviceOptional, rDriver}},
	{"vbcable", Advice{AdviceOptional, rDriver}},
	{"vb-audio", Advice{AdviceOptional, rDriver}},
	{"ivcam", Advice{AdviceOptional, rOnlyIf}},
	{"spacedesk", Advice{AdviceOptional, rOnlyIf}},
	{"duet", Advice{AdviceOptional, rOnlyIf}},
	{"sunshine", Advice{AdviceOptional, rOnlyIf}},
	{"moonlight", Advice{AdviceSafeOff, rHelper}},
	{"kdeconnect", Advice{AdviceOptional, rOnlyIf}},
	{"scrcpy", Advice{AdviceSafeOff, rHelper}},
	{"anlink", Advice{AdviceOptional, rOnlyIf}},
	{"wps", Advice{AdviceOptional, rUnused}},
	{"hisuite", Advice{AdviceOptional, rUnused}},
	{"i4tools", Advice{AdviceSafeOff, rOnDemand}},
	{"itools", Advice{AdviceSafeOff, rOnDemand}},
	{"ludashi", Advice{AdviceSafeOff, rOnDemand}},
	{"frp", Advice{AdviceOptional, rOnlyIf}},
	{"ngrok", Advice{AdviceOptional, rOnlyIf}},
	{"fiddler", Advice{AdviceSafeOff, rOnDemand}},
	{"charles", Advice{AdviceSafeOff, rOnDemand}},
	{"wireshark", Advice{AdviceSafeOff, rOnDemand}},
	{"obs64", Advice{AdviceSafeOff, rOnDemand}},
	{"obs-studio", Advice{AdviceSafeOff, rOnDemand}},
	{"obs studio", Advice{AdviceSafeOff, rOnDemand}},
	{"streamlabs", Advice{AdviceSafeOff, rOnDemand}},
	{"calibre", Advice{AdviceSafeOff, rHelper}},
	{"hidusbf", Advice{AdviceCaution, rDriver}},
	{"npcap", Advice{AdviceKeep, "Packet capture driver support — Wireshark/Nmap need it"}},
	{"sandboxie", Advice{AdviceOptional, rUnused}},
	{"adskidentity", Advice{AdviceOptional, rOnlyIf}},
	{"adskaccess", Advice{AdviceSafeOff, rHelper}},
	{"autodesk", Advice{AdviceSafeOff, rHelper}},
	{"epson", Advice{AdviceOptional, rPrint}},
	{"canon", Advice{AdviceOptional, rPrint}},
	{"brother", Advice{AdviceOptional, rPrint}},
	{"hp print", Advice{AdviceOptional, rPrint}},
	{"ccleaner", Advice{AdviceSafeOff, rOnDemand}},
	{"speccy", Advice{AdviceSafeOff, rHelper}},
	{"recuva", Advice{AdviceSafeOff, rHelper}},
	{"defraggler", Advice{AdviceSafeOff, rHelper}},
	{"windirstat", Advice{AdviceSafeOff, rHelper}},
	{"treesize", Advice{AdviceSafeOff, rHelper}},
	{"wiztree", Advice{AdviceSafeOff, rHelper}},
	{"bandizip", Advice{AdviceSafeOff, rHelper}},
	{"7-zip", Advice{AdviceSafeOff, rHelper}},
	{"winrar", Advice{AdviceSafeOff, rHelper}},
}

// AdviceFor returns the suggestion for a startup entry: services consult
// the name table first, then everything falls back to pattern-matching
// name/exe/command/publisher. Windows-owned components that match
// nothing get a "leave it" note — the system list errs toward preserving
// what it doesn't recognize.
func AdviceFor(it Item) Advice {
	svc := it.value
	if svc == "" && it.Kind == KindService {
		svc = strings.TrimPrefix(it.Location, "Service ")
	}
	if it.Kind == KindService && svc != "" {
		if a, ok := adviceServices[strings.ToLower(svc)]; ok {
			return a
		}
		if it.Package != "" {
			return Advice{AdviceOptional, rPackaged}
		}
	}
	hay := strings.ToLower(it.Name + " " + it.Exe + " " + it.Command + " " + it.Publisher)
	for _, e := range adviceApps {
		if strings.Contains(hay, e.match) {
			return e.adv
		}
	}
	if (it.Kind == KindService || it.Kind == KindTask) && it.Windows {
		return Advice{AdviceCaution, rWinUnk}
	}
	return Advice{AdviceUnknown, rUnknown}
}
