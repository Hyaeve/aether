<script setup>
import {
  PanelLeft, Star, List, LayoutGrid, Settings, FolderSync, CloudDownload, Logs, TableProperties, LockKeyhole,
  Gauge, UserPlus, FolderPlus, Sparkles, BrainCircuit, Tags, Film, ListFilter, Share2, ArrowLeftRight,
  Activity, ArchiveRestore, ArrowDown, ArrowLeft, ArrowRight, ArrowUp, ArrowUpRight,
  Bell, Box, Check, ChevronDown, ChevronUp, ChevronRight, ChevronsUpDown, Circle, CircleAlert,
  CircleCheck, CircleDashed, Cloud, Database, Download, Ellipsis, Eye, EyeOff,
  FileVideo, Folder, FolderOpen, FolderTree, HardDrive, HardDriveDownload, Info,
  Layers3, LayoutDashboard, Link, ListTodo, LoaderCircle, LogOut, Menu, Monitor,
  Moon, Network, Pause, Pencil, Play, Plus, RefreshCw, Save, ScanSearch, ScrollText, Search,
  Server, Settings2, ShieldCheck, Square, Sun, Trash2, UserRound, Waypoints, Wrench, X
} from 'lucide-vue-next'
import { computed } from 'vue'
const icons = {
  PanelLeft, Star, List, LayoutGrid, Settings, FolderSync, CloudDownload, Logs, TableProperties, LockKeyhole,
  Gauge, UserPlus, FolderPlus, Sparkles, BrainCircuit, Tags, Film, ListFilter, Share2, ArrowLeftRight,
  Activity, ArchiveRestore, ArrowDown, ArrowLeft, ArrowRight, ArrowUp, ArrowUpRight,
  Bell, Box, Check, ChevronDown, ChevronUp, ChevronRight, ChevronsUpDown, Circle, CircleAlert,
  CircleCheck, CircleDashed, Cloud, Database, Download, Ellipsis, Eye, EyeOff,
  FileVideo, Folder, FolderOpen, FolderTree, HardDrive, HardDriveDownload, Info,
  Layers3, LayoutDashboard, Link, ListTodo, LoaderCircle, LogOut, Menu, Monitor,
  Moon, Network, Pause, Pencil, Play, Plus, RefreshCw, Save, ScanSearch, ScrollText, Search,
  Server, Settings2, ShieldCheck, Square, Sun, Trash2, UserRound, Waypoints, Wrench, X
}
const props = defineProps({ name: String, size: { type: Number, default: 18 } })
const icon = computed(() => icons[props.name] || icons.Circle)
</script>
<template><span v-if="name === 'WebDAV'" class="dav-symbol" :style="{ width: `${size}px`, height: `${size}px` }" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d="M5 21C.5 21 .1 14 3.2 11.7C1.2 7.4 5.2 3.3 8.3 5.4C9.7-.7 17-.5 18.6 5.7C23.3 4.8 24.5 11 21.8 13C25 17 21.8 21 19 21Z" /></svg><span>DAV</span></span><component v-else :is="icon" :size="size" :stroke-width="1.7" aria-hidden="true" /></template>
