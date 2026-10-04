<script setup>
import {
  Settings, FolderSync, Router,
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
  Settings, FolderSync, Router,
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
<template><component :is="icon" :size="size" :stroke-width="1.7" aria-hidden="true" /></template>
