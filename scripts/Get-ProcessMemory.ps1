param([int]$ProcessId)
Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class MemoryProbe {
 [StructLayout(LayoutKind.Sequential)] public struct Counters {
  public uint Size, Faults;
  public UIntPtr PeakWorkingSet, WorkingSet, PeakPagedPool, PagedPool, PeakNonPagedPool, NonPagedPool, Pagefile, PeakPagefile, PrivateUsage, PrivateWorkingSet, SharedCommit;
 }
 [DllImport("psapi.dll", SetLastError=true)] public static extern bool GetProcessMemoryInfo(IntPtr handle, ref Counters counters, uint size);
}
'@
$p = Get-Process -Id $ProcessId
$c = New-Object MemoryProbe+Counters
$c.Size = [Runtime.InteropServices.Marshal]::SizeOf($c)
if (![MemoryProbe]::GetProcessMemoryInfo($p.Handle, [ref]$c, $c.Size)) { throw 'Memory query failed' }
[pscustomobject]@{
 privateWorkingSet = $c.PrivateWorkingSet.ToUInt64()
 workingSet = $c.WorkingSet.ToUInt64()
 privateBytes = $c.PrivateUsage.ToUInt64()
 sharedCommit = $c.SharedCommit.ToUInt64()
}
