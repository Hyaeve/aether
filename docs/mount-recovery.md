# Docker部署的FUSE断连

`Transport endpoint is not connected: /NetDisk/AetherDrive`对应FUSE连接断开，路径上可能仍有旧挂载。它不等于媒体文件不存在，也不能仅靠增加端口或改文件权限解决。进程/容器退出、OOM或连接被中止都需要检查；没有现场日志，不能确定是哪一项导致本次断连。[Linux FUSE说明](https://docs.kernel.org/filesystems/fuse/fuse.html)

本地挂载配置填写父目录`/NetDisk`，界面会追加`/AetherDrive`，实际媒体库路径是`/NetDisk/AetherDrive`。如果填`/NetDisk/AetherDrive`，实际会再追加一层，按卡片展示的完整路径核对。

两个Docker容器应绑定父目录，而非将已挂载的子目录固定绑定到Emby。Aether需要向宿主机传播挂载，Emby需要接收宿主机的新挂载。下面是对应服务的卷配置片段，不是完整Compose：

```yaml
# Aether
volumes:
  - /NetDisk:/NetDisk:rshared

# Emby
volumes:
  - /NetDisk:/NetDisk:rslave
```

宿主机源挂载必须支持共享传播；Docker的默认`rprivate`不会传播后续的子挂载。`pid: host`和网络host模式不能替代挂载传播。直接绑定`/NetDisk/AetherDrive`的旧Emby容器可能仍引用旧FUSE对象，恢复Aether挂载后，应按父目录映射重建该Emby容器以重新取得路径。[Docker绑定挂载与传播](https://docs.docker.com/engine/storage/bind-mounts/#configure-bind-propagation)

在现场先读取状态，保留报错前后的日志（容器名称按实际替换）：

```sh
docker inspect Aether --format '{{json .State}}'
docker inspect Aether --format '{{json .Mounts}}'
docker logs --since 10m Aether
findmnt -T /NetDisk -o TARGET,FSTYPE,SOURCE,PROPAGATION
findmnt -T /NetDisk/AetherDrive -o TARGET,FSTYPE,SOURCE,PROPAGATION
```

先停止Emby对该目录的读取，再在Aether卸载/重新挂载。程序在启动或重新挂载时，只会对配置中同一路径、同一`fuse.aether`类型、同一`aether:挂载名称`来源且明确返回`ENOTCONN`的旧挂载做延迟卸载；操作前后复核内核挂载ID。健康挂载、其他来源、重叠多层挂载或身份变化一律保留并报错，不删除挂载目录或媒体文件。FUSE服务意外退出会显示断连错误，不再只显示正常退出。没有自动定时强制卸载，也不会在Windows本机修改远端Docker服务。

这个恢复分支及Docker映射仍需在实际Linux/NAS部署验证；Windows协议模拟和Linux交叉编译不能替代真实FUSE、内核挂载命名空间和Emby读取验收。未取得现场日志前，不能宣称本次Emby报错已现场恢复。
