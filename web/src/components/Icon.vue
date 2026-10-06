<script setup>
import {
  Copy, FolderInput, GripVertical, Power,
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
  Copy, FolderInput, GripVertical, Power,
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
<template><span v-if="name === 'WebDAV'" class="dav-symbol" :style="{ width: `${size}px`, height: `${size}px` }" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d="M5 21C-.5 21-.5 7 7 9C7-1 17-1 17 9C24.5 7 24.5 21 19 21Z" /></svg><span>DAV</span></span><component v-else :is="icon" :size="size" :stroke-width="1.7" aria-hidden="true" /></template>
