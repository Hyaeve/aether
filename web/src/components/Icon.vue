<script setup>
import {
  ListVideo, FileArchive, EllipsisVertical, Copy, FolderInput, GripVertical, Power,
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
  ListVideo, FileArchive, EllipsisVertical, Copy, FolderInput, GripVertical, Power,
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
<template><span v-if="name === 'WebDAV'" class="dav-symbol" :style="{ width: `${size}px`, height: `${size}px` }" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M6 20a4.5 4.5 0 0 1-4.5-4.5V12a4 4 0 0 1 6-3.5 4.5 4.5 0 0 1 9 0 4 4 0 0 1 6 3.5v3.5A4.5 4.5 0 0 1 18 20Z" /></svg><span>DAV</span></span><component v-else :is="icon" :size="size" :stroke-width="1.7" aria-hidden="true" /></template>
