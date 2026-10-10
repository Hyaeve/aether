# 反向代理与登录

“跨站请求已拒绝”表示修改请求的浏览器 Origin 与后端识别的访问地址不同，常见于反代把 Host 改成容器地址，或 HTTPS 在代理终止后后端只收到 HTTP。它不表示账号密码错误。

Aether 规范化主机名大小写和默认端口，仍拒绝真正的跨站修改请求。设置中的服务地址 `PublicURL` 是明确允许的外部访问源；修改时使用后台，不要手工改带完整性校验的 JSON。

反代应覆盖客户端传来的转发头，并发送单值 `X-Forwarded-Host` 和 `X-Forwarded-Proto`；若保留原 Host，只需后者。只有来自可信代理的连接才使用这些头。默认信任回环和本机接口地址，不信任整个局域网；容器内可用 `AETHER_TRUSTED_PROXIES=172.18.0.2/32` 显式添加实际代理 IP/CIDR，不要笼统放开所有来源。单独的客户端伪造转发头不会放行跨站请求。

例如 Nginx 中保留外部地址：

```nginx
proxy_set_header Host $http_host;
proxy_set_header X-Forwarded-Host $http_host;
proxy_set_header X-Forwarded-Proto $scheme;
```

代理地址必须与容器实际看到的连接来源一致；多层代理应在最靠近 Aether 的可信代理处统一为最终外部地址，而不是追加多个来源。不要通过关闭跨站保护修复登录。
