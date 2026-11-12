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
	"audioendpointbuilder":         {AdviceKeep, rCore},
	"audiosrv":                     {AdviceKeep, rCore},
	"bfe":                          {AdviceKeep, rCore},
	"brokerinfrastructure":         {AdviceKeep, rCore},
	"camsvc":                       {AdviceKeep, rCore},
	"cbdhsvc":                      {AdviceKeep, rCore},
	"cdpsvc":                       {AdviceKeep, rCore},
	"cdpusersvc":                   {AdviceKeep, rCore},
	"clipsvc":                      {AdviceKeep, rCore},
	"coremessagingregistrar":       {AdviceKeep, rCore},
	"cryptsvc":                     {AdviceKeep, rCore},
	"cryptsp":                      {AdviceKeep, rCore},
	"dcomlaunch":                   {AdviceKeep, rCore},
	"deviceassociationservice":     {AdviceKeep, rCore},
	"devicepickerusersvc":          {AdviceKeep, rCore},
	"devicenpickupsvc":             {AdviceKeep, rCore},
	"devicesflowusersvc":           {AdviceKeep, rCore},
	"dhcp":                         {AdviceKeep, rCore},
	"dispbrokerdesktopsvc":         {AdviceKeep, rCore},
	"displayenhancementservice":    {AdviceKeep, rCore},
	"dnscache":                     {AdviceKeep, rCore},
	"dps":                          {AdviceKeep, rCore},
	"dsrole":                       {AdviceKeep, rCore},
	"dusmsvc":                      {AdviceKeep, rCore},
	"eventlog":                     {AdviceKeep, rCore},
	"eventsystem":                  {AdviceKeep, rCore},
	"fontcache":                    {AdviceKeep, rCore},
	"gpsvc":                        {AdviceKeep, rCore},
	"hnetcfg":                      {AdviceKeep, rCore},
	"installservice":               {AdviceKeep, rCore},
	"keyiso":                       {AdviceKeep, rCore},
	"lanmanworkstation":            {AdviceKeep, rCore},
	"licensemanager":               {AdviceKeep, rCore},
	"mpssvc":                       {AdviceKeep, rCore},
	"netlogon":                     {AdviceKeep, rCore},
	"netman":                       {AdviceKeep, rCore},
	"netprofm":                     {AdviceKeep, rCore},
	"netsetupsvc":                  {AdviceKeep, rCore},
	"networkconnectionbroker":      {AdviceKeep, rCore},
	"ngcsvc":                       {AdviceKeep, rCore},
	"nlasvc":                       {AdviceKeep, rCore},
	"nsi":                          {AdviceKeep, rCore},
	"plugplay":                     {AdviceKeep, rCore},
	"power":                        {AdviceKeep, rCore},
	"profsvc":                      {AdviceKeep, rCore},
	"qrssvc":                       {AdviceKeep, rCore},
	"rpceptmapper":                 {AdviceKeep, rCore},
	"rpcss":                        {AdviceKeep, rCore},
	"samss":                        {AdviceKeep, rCore},
	"schedule":                     {AdviceKeep, rCore},
	"securityhealthservice":        {AdviceKeep, rSecurity},
	"sens":                         {AdviceKeep, rCore},
	"shellhwdetection":             {AdviceKeep, rCore},
	"smbsvcs":                      {AdviceKeep, rCore},
	"staterepository":              {AdviceKeep, rCore},
	"swprv":                        {AdviceKeep, rCore},
	"systemeventsbroker":           {AdviceKeep, rCore},
	"themes":                       {AdviceKeep, rCore},
	"timebroker":                   {AdviceKeep, rCore},
	"tokenbroker":                  {AdviceKeep, rCore},
	"trustedinstaller":             {AdviceKeep, rCore},
	"udkusersvc":                   {AdviceKeep, rCore},
	"usermanager":                  {AdviceKeep, rCore},
	"usosvc":                       {AdviceKeep, rCore},
	"uxsms":                        {AdviceKeep, rCore},
	"vaultsvc":                     {AdviceKeep, rCore},
	"vss":                          {AdviceKeep, rCore},
	"w32time":                      {AdviceKeep, rCore},
	"warpjitsvc":                   {AdviceKeep, rCore},
	"wbiosrvc":                     {AdviceKeep, rCore},
	"wcmsvc":                       {AdviceKeep, rCore},
	"wdiservicehost":               {AdviceKeep, rCore},
	"wdisystemhost":                {AdviceKeep, rCore},
	"windefend":                    {AdviceKeep, rSecurity},
	"winhttpautoproxysvc":          {AdviceKeep, rCore},
	"winmgmt":                      {AdviceKeep, rCore},
	"wlansvc":                      {AdviceKeep, rCore},
	"wmiapsrv":                     {AdviceKeep, rCore},
	"wpnuserservice":               {AdviceKeep, rCore},
	"wpnservice":                   {AdviceKeep, rCore},
	"wscsvc":                       {AdviceKeep, rCore},
	"wsappx":                       {AdviceKeep, rCore},
	"wuauserv":                     {AdviceKeep, rCore},
	"wudfrd":                       {AdviceKeep, rCore},
	"appinfo":                      {AdviceKeep, rCore},
	"appreadiness":                 {AdviceKeep, rCore},
	"defragsvc":                    {AdviceKeep, rCore},
	"netconnectioncentralaccessor": {AdviceKeep, rCore},
	"wrevelstsvc":                  {AdviceKeep, rCore},

	// Windows — safe to disable for most people
	"diagtrack":        {AdviceSafeOff, rTelemetry},
	"dmwappushservice": {AdviceSafeOff, rTelemetry},
	"diagnosticshub.standardcollector.service": {AdviceSafeOff, rTelemetry},
	"retsvc":               {AdviceSafeOff, rTelemetry},
	"wercplsupport":        {AdviceSafeOff, rTelemetry},
	"wersvc":               {AdviceSafeOff, rTelemetry},
	"stordiag":             {AdviceSafeOff, rTelemetry},
	"sgrmbroker":           {AdviceSafeOff, rTelemetry},
	"dosvc":                {AdviceSafeOff, rWinOpt},
	"mapsbroker":           {AdviceSafeOff, rWinOpt},
	"fax":                  {AdviceSafeOff, rWinOpt},
	"retaildemo":           {AdviceSafeOff, rWinOpt},
	"lmhosts":              {AdviceSafeOff, rWinOpt},
	"ssdfsrv":              {AdviceSafeOff, rWinOpt},
	"ssdpsrv":              {AdviceSafeOff, rWinOpt},
	"upnphost":             {AdviceSafeOff, rWinOpt},
	"wcncsvc":              {AdviceSafeOff, rWinOpt},
	"webclient":            {AdviceSafeOff, rWinOpt},
	"wecsvc":               {AdviceSafeOff, rWinOpt},
	"wisvc":                {AdviceSafeOff, rWinOpt},
	"workfolderssvc":       {AdviceSafeOff, rWinOpt},
	"remoteregistry":       {AdviceSafeOff, rWinOpt},
	"winrm":                {AdviceSafeOff, rRemote},
	"peerdistsvc":          {AdviceSafeOff, rWinOpt},
	"pnrpauto":             {AdviceSafeOff, rWinOpt},
	"pnrpsvc":              {AdviceSafeOff, rWinOpt},
	"p2pimsvc":             {AdviceSafeOff, rWinOpt},
	"p2psvc":               {AdviceSafeOff, rWinOpt},
	"axinstsv":             {AdviceSafeOff, rWinOpt},
	"browser":              {AdviceSafeOff, rWinOpt},
	"napagent":             {AdviceSafeOff, rWinOpt},
	"nettcpportsharing":    {AdviceSafeOff, rWinOpt},
	"snmptrap":             {AdviceSafeOff, rWinOpt},
	"spatialdata":          {AdviceSafeOff, rWinOpt},
	"tapisrv":              {AdviceSafeOff, rWinOpt},
	"wephostsvc":           {AdviceSafeOff, rWinOpt},
	"whesvc":               {AdviceSafeOff, rWinOpt},
	"wpcmonsvc":            {AdviceSafeOff, rWinOpt},
	"wpcsvc":               {AdviceSafeOff, rWinOpt},
	"homegroupprovider":    {AdviceSafeOff, rWinOpt},
	"homegrouplistener":    {AdviceSafeOff, rWinOpt},
	"wbengine":             {AdviceSafeOff, rBackup},
	"sdrsvc":               {AdviceSafeOff, rBackup},
	"dssvc":                {AdviceSafeOff, rWinOpt},
	"davauthservice":       {AdviceSafeOff, rWinOpt},
	"cscservice":           {AdviceSafeOff, rWinOpt},
	"wmpnetworksvc":        {AdviceSafeOff, rMedia},
	"sshd":                 {AdviceSafeOff, rRemote},
	"perceptionsimulation": {AdviceSafeOff, rWinOpt},
	"mixedrealityopenxr":   {AdviceSafeOff, rWinOpt},
	"nosxp":                {AdviceSafeOff, rWinOpt},
	"spectrum":             {AdviceSafeOff, rWinOpt},
	"phonesvc":             {AdviceSafeOff, rWinOpt},
	"pushtoinstall":        {AdviceSafeOff, rWinOpt},
	"xboxgipsvc":           {AdviceSafeOff, rWinOpt},
	"xblgamesave":          {AdviceSafeOff, rWinOpt},
	"xboxnetapisvc":        {AdviceSafeOff, rWinOpt},
	"wephostsvcs":          {AdviceSafeOff, rWinOpt},

	// Windows — optional, think first
	"wsearch":               {AdviceOptional, rIndex},
	"sysmain":               {AdviceOptional, rWinOpt},
	"spooler":               {AdviceOptional, rPrint},
	"printnotify":           {AdviceOptional, rPrint},
	"iphlpsvc":              {AdviceOptional, rOnlyIf},
	"lanmanserver":          {AdviceOptional, rOnlyIf},
	"sharedaccess":          {AdviceOptional, rOnlyIf},
	"icssvc":                {AdviceOptional, rOnlyIf},
	"dot3svc":               {AdviceOptional, rOnlyIf},
	"seclogon":              {AdviceOptional, rOnlyIf},
	"ikeext":                {AdviceOptional, rOnlyIf},
	"policyagent":           {AdviceOptional, rOnlyIf},
	"rasauto":               {AdviceOptional, rRemote},
	"rasman":                {AdviceOptional, rRemote},
	"remoteaccess":          {AdviceOptional, rRemote},
	"remoteaccessservice":   {AdviceOptional, rRemote},
	"termservice":           {AdviceOptional, rRemote},
	"umrdpservice":          {AdviceOptional, rRemote},
	"sessionenv":            {AdviceOptional, rRemote},
	"pcasvc":                {AdviceOptional, rUnused},
	"wlidsvc":               {AdviceOptional, rOnlyIf},
	"efssvc":                {AdviceOptional, rOnlyIf},
	"fdphost":               {AdviceOptional, rOnlyIf},
	"fdrespub":              {AdviceOptional, rOnlyIf},
	"scardsvr":              {AdviceOptional, rOnlyIf},
	"scdeviceenum":          {AdviceOptional, rOnlyIf},
	"certpropsvc":           {AdviceOptional, rOnlyIf},
	"tabletinputservice":    {AdviceOptional, rOnlyIf},
	"stisvc":                {AdviceOptional, rOnlyIf},
	"sensordataservice":     {AdviceOptional, rWinOpt},
	"sensorservice":         {AdviceOptional, rUnused},
	"sensrsvc":              {AdviceOptional, rWinOpt},
	"naturalauthentication": {AdviceOptional, rOnlyIf},
	"xblauthmanager":        {AdviceOptional, rOnlyIf},
	"trkwks":                {AdviceOptional, rOnlyIf},
	"wmiacpi":               {AdviceCaution, rDriver},
	"waledigest":            {AdviceCaution, rCore},
	"wdisvc":                {AdviceCaution, rWinOpt},
	"edgeupdate":            {AdviceSafeOff, dBrowserUpd},
	"edgeupdatem":           {AdviceSafeOff, dBrowserUpd},

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
	{"onedrive per-machine", Advice{AdviceSafeOff, dOneDriveUpd}},
	{"onedrive reporting", Advice{AdviceSafeOff, dOneDriveUpd}},
	{"onedrive startup task", Advice{AdviceSafeOff, dOneDriveUpd}},

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
	{"outlook", Advice{AdviceOptional, rMailOff}},

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
	{"onenote", Advice{AdviceOptional, rHelper}},
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
