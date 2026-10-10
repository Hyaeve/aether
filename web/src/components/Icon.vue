<script setup>
import {
  ListOrdered, Repeat, Repeat1, Volume2, VolumeX, FileSymlink, Shuffle, SkipBack, SkipForward,
  ChevronLeft, ZoomIn, ZoomOut, Maximize, Minimize, RotateCw,
  FilePenLine, FileJson, FileAudio2, FileVideo2, FileText,
  RotateCcw, Music, Image, Tv, BriefcaseBusiness, Globe, House, ExternalLink, CornerDownLeft,
  History, FileClock, ListVideo, FileArchive, EllipsisVertical, Copy, FolderInput, GripVertical, Power,
  PanelLeft, Star, List, LayoutGrid, Settings, FolderSync, CloudDownload, Logs, TableProperties, LockKeyhole,
  Gauge, UserPlus, FolderPlus, Sparkles, BrainCircuit, Tags, Film, ListFilter, Share2, ArrowLeftRight,
  Activity, ArchiveRestore, ArrowDown, ArrowLeft, ArrowRight, ArrowUp, ArrowUpRight,
  Bell, Box, Check, CheckCheck, ChevronDown, ChevronUp, ChevronRight, ChevronsUpDown, Circle, CircleAlert,
  CircleCheck, CircleDashed, Cloud, Database, Download, Upload, Ellipsis, Eye, EyeOff,
  FileVideo, Folder, FolderOpen, FolderTree, HardDrive, HardDriveDownload, Info,
  Layers3, LayoutDashboard, Link, ListTodo, LoaderCircle, LogOut, Menu, Monitor,
  Moon, Network, Pause, Pencil, Play, Plus, RefreshCw, Save, ScanSearch, ScrollText, Search,
  Server, Settings2, ShieldCheck, Square, Sun, Trash2, UserRound, Waypoints, Wrench, X
} from 'lucide-vue-next'
import { computed, h } from 'vue'
import EmbyNoticeIcon from './EmbyNoticeIcon.vue'
const icons = {
  ListOrdered, Repeat, Repeat1, Volume2, VolumeX, FileSymlink, Shuffle, SkipBack, SkipForward,
  ChevronLeft, ZoomIn, ZoomOut, Maximize, Minimize, RotateCw,
  FilePenLine, FileJson, FileAudio2, FileVideo2, FileText,
  EmbyNotice: EmbyNoticeIcon,
  TMDB: () => h('img', { src: '/providers/tmdb.svg', alt: '', width: 32, height: 32, style: 'object-fit:contain' }),
  RotateCcw, Music, Image, Tv, BriefcaseBusiness, Globe, House, ExternalLink, CornerDownLeft,
  History, FileClock, ListVideo, FileArchive, EllipsisVertical, Copy, FolderInput, GripVertical, Power,
  PanelLeft, Star, List, LayoutGrid, Settings, FolderSync, CloudDownload, Logs, TableProperties, LockKeyhole,
  Gauge, UserPlus, FolderPlus, Sparkles, BrainCircuit, Tags, Film, ListFilter, Share2, ArrowLeftRight,
  Activity, ArchiveRestore, ArrowDown, ArrowLeft, ArrowRight, ArrowUp, ArrowUpRight,
  Bell, Box, Check, CheckCheck, ChevronDown, ChevronUp, ChevronRight, ChevronsUpDown, Circle, CircleAlert,
  CircleCheck, CircleDashed, Cloud, Database, Download, Upload, Ellipsis, Eye, EyeOff,
  FileVideo, Folder, FolderOpen, FolderTree, HardDrive, HardDriveDownload, Info,
  Layers3, LayoutDashboard, Link, ListTodo, LoaderCircle, LogOut, Menu, Monitor,
  Moon, Network, Pause, Pencil, Play, Plus, RefreshCw, Save, ScanSearch, ScrollText, Search,
  Server, Settings2, ShieldCheck, Square, Sun, Trash2, UserRound, Waypoints, Wrench, X
}
const props = defineProps({ name: String, size: { type: Number, default: 18 } })
const icon = computed(() => icons[props.name] || icons.Circle)
</script>
<template><span v-if="name === 'WebDAV'" class="dav-symbol" :style="{ width: `${size}px`, height: `${size}px` }" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M6 20C3.2 20 1.5 18 1.5 15.4C1.5 12.8 3.1 11 5.5 10.8C5 7.7 6.9 4.5 10.3 4.5C13.1 4.5 15 6.5 15.2 9C18.9 7.6 22.5 10.2 22.5 14C22.5 17.6 20.5 20 17.3 20Z" /></svg><span>DAV</span></span><component v-else :is="icon" :size="size" :stroke-width="1.7" aria-hidden="true" /></template>
