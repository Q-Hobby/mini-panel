# Nhật ký thay đổi

Mọi thay đổi đáng kể của Mini Panel được ghi ở đây. Số phiên bản theo [Semantic Versioning](https://semver.org/lang/vi/).

Bản chính bằng tiếng Anh: [CHANGELOG.md](CHANGELOG.md). Có thêm bản [tiếng Trung](CHANGELOG.zh.md).

## [1.2.0] - 2026-11-18

### Thêm

- Cập nhật panel từ giao diện (Cài đặt, mục Cập nhật) hoặc bằng `mpn update -apply`. `SHA256SUMS` của mỗi bản phát hành được ký bằng minisign, và panel chỉ nhận bản có chữ ký đúng với khoá dựng sẵn trong chương trình. Phần chú thích có ký ghi số phiên bản, nên không ai đem được bản cũ ra giả làm bản mới. Bản đang chạy được giữ ở `/usr/local/bin/mpn.prev`, và nếu panel không trả lời sau khi cập nhật thì bản đó được đặt lại.
- Chép bản sao lưu ra ngoài máy chủ, giờ dùng rclone: Google Drive, S3 (Cloudflare R2, AWS, Wasabi…), Backblaze B2, SFTP, WebDAV (Nextcloud, ownCloud) và FTP/FTPS. Kết nối chỉ được lưu khi panel đã ghi, đọc lại và xoá được một tệp thử. Biểu mẫu chỉ chuyển cho rclone những trường có trong danh sách cố định, nên không có tuỳ chọn nào chạy được lệnh hay đọc tệp của máy.
- Lựa chọn lấy địa chỉ thật của khách khi site đi qua Cloudflare (Bảo mật, hoặc `mpn security cloudflare -on -apply`). nginx lấy địa chỉ trong `CF-Connecting-IP`, nhưng chỉ trên kết nối đến từ dải mạng Cloudflare công bố. Mặc định tắt.
- Cài MariaDB hoặc MySQL thì phpMyAdmin được cài kèm, để tab Cơ sở dữ liệu mở được ngay.
- `mpn panel migrate` đưa máy cài bằng bản cũ về đúng cấu hình của bản cài mới. Bộ cài và `mpn update` tự chạy lệnh này.

### Đổi

- Panel nghe trên socket Unix `/run/mpn-web/web.sock` do systemd tạo và giữ (`mpn-web.socket`), không còn nghe `127.0.0.1:8780`. Chỉ nginx và phpMyAdmin (nhóm `mpn-front`) kết nối được. Socket vẫn còn khi panel khởi động lại, nên không chương trình nào chiếm được chỗ của panel. Địa chỉ khách trong `X-Real-IP` chỉ được tin khi panel nghe trên socket đó.
- Site Node.js và Python mới (Debian, Ubuntu) nghe trên socket Unix `/run/mpn-app/<site>/app.sock`, không mở cổng TCP. Chỉ nginx kết nối được. Đường dẫn socket nằm trong biến `SOCKET`, và `PORT` cũng mang đúng đường dẫn đó, vì `listen(process.env.PORT)` của Node.js nhận được đường dẫn. Để trống ô cổng nghĩa là dùng socket, còn nhập số cổng thì app nghe `127.0.0.1` như trước. Site đã có giữ nguyên cổng. Trên họ RHEL, app vẫn dùng cổng.
- Trang thiết lập `/setup` chỉ mở bằng liên kết có khoá dùng một lần. Bộ cài in liên kết này, lời chào khi SSH vào máy cũng hiện nó cho tới khi có tài khoản, và `mpn web setup-link` in lại khi cần. Trước đây ai mở `/setup` trước thì người đó tạo tài khoản.
- Node.js chỉ được cài khi `SHASUMS256.txt` có chữ ký hợp lệ của người phát hành Node.js. Chữ ký được kiểm bằng `gpgv` với 29 khoá dựng sẵn trong mpn, nên nodejs.org hay một bản sao của nó không tự chọn được khoá. `gpgv` có trong bước gói cơ bản.
- MANIFEST của bản sao lưu có SHA-256 của từng tệp và một HMAC ký bằng khoá riêng của máy (`/etc/mpn/backup.key`, quyền 0600). Bản tải về từ kho ngoài phải có chữ ký của chính máy này, và mỗi tệp phải khớp tổng của nó.
- vHost viết tay không được ghi log ra ngoài `/var/log/mpn/sites/<site>/`. Các chỉ thị đọc tệp hay ảnh hưởng tới cả máy (`include`, `load_module`, `ssl_certificate` ngoài thư mục chứng chỉ của site, `*_temp_path`, `*_cache_path`, `user`, `pid`…) bị từ chối. Lý do là nginx mở tệp log bằng quyền root trước khi đổi sang tài khoản worker.
- Laravel ghi `DB_HOST=localhost` (qua socket Unix) như WordPress và CodeIgniter.
- Mã QR của xác thực hai bước được vẽ bằng mã riêng của panel, thay thư viện go-qrcode đã ngừng phát triển.
- Lệnh và thao tác chạy bằng root tạo thư mục trong home của site qua mô tả thư mục đã mở và không theo liên kết tượng trưng, nên tài khoản site không dùng symlink đưa root sang tệp khác được.
- Không tạo được cơ sở dữ liệu có tên thuộc về site khác rõ hơn (ví dụ `blog_shop` khi có site `blog-shop`).
- Khi quá nhiều lượt đăng nhập cùng chờ băm mật khẩu, panel trả lời 429 ngay thay vì xếp hàng, để một đợt dò mật khẩu không chiếm hết CPU.
- Mã nguồn công khai có kèm test, và GitHub Actions chạy gofmt, vet, test và govulncheck ở mỗi lần đẩy.

### Lưu ý khi nâng cấp

- Bản sao lưu làm bằng 1.1.x chưa có chữ ký, nên không tải về từ kho ngoài được nữa. Bản sao lưu nằm trên máy vẫn khôi phục được như cũ.
- Site có vHost viết tay đang `include` tệp khác hoặc ghi log ra chỗ khác sẽ báo lỗi ở lần áp dụng tiếp theo, cho tới khi sửa phần đó.

## [1.1.1] - 2026-11-14

### Sửa

- Sao lưu giờ chép cả thư mục `htdocs` khi thư mục gốc web nằm bên dưới nó, ví dụ `htdocs/public` của Laravel, Flarum hay Symfony. Trước đây bản sao lưu chỉ có `public/` và thiếu `config.php`/`.env`, `storage` (tệp tải lên) và `vendor`, nên khôi phục từ đó không chạy lại được site. Site có thư mục gốc là `htdocs` không đổi gì.
- MANIFEST của các bản sao lưu đó có thêm dòng `files_root=htdocs`, và khôi phục dựa vào dòng này: bản sao lưu cũ (chỉ có thư mục gốc web) vẫn được bung về thư mục gốc web như trước. mpn 1.1.0 trở về trước không đọc được MANIFEST mới, nên hãy nâng cấp trước khi khôi phục các bản sao lưu mới.

## [1.1.0] - 2026-11-13

### Thêm

- Chạy trên AlmaLinux, Rocky Linux và CentOS Stream 9 và 10, cả x86_64 lẫn arm64. Nginx 1.28.3 lấy từ nginx.org và khoá bằng `dnf versionlock`; PHP 7.4 trở lên từ Remi (kèm EPEL); PHP 7.0 đến 7.3, module ModSecurity và OWASP CRS 4.25.2 lấy từ bản [el-r1](https://github.com/marixdev/mpn-packages/releases/tag/el-r1) của mpn-packages; PageSpeed từ bản [nginx-1.28.3-r3](https://github.com/marixdev/mpn-packages/releases/tag/nginx-1.28.3-r3). Mọi tệp tải về đều được kiểm SHA-256 đã ghim.
- MariaDB lấy từ rpm.mariadb.org và MySQL từ repo.mysql.com; khoá ký của hai kho được kiểm theo fingerprint. EL10 chỉ có MySQL 8.4.
- SELinux vẫn bật: panel gán nhãn cho thư mục website, log, socket PHP-FPM, venv của site Python, Node.js, hộp thư và cổng của panel, và bật `httpd_can_network_connect` để site gọi được cơ sở dữ liệu và mạng ngoài.
- Bộ cài tắt firewalld, vì panel quản lý tường lửa bằng nftables.
- Mail trên họ RHEL: mật khẩu hộp thư băm bằng bcrypt (`BLF-CRYPT`), vì Dovecot ở đó không có argon2; OpenDKIM nghe ở cổng 8891 trên loopback, vì chính sách SELinux không cho Postfix nối vào socket của nó.

### Đổi

- Module nginx giờ nằm ở repo mpn-packages (trước là mpn-nginx-modules; địa chỉ cũ vẫn chuyển tiếp tới đây).

## [1.0.3] - 2026-11-13

### Thêm

- Chạy trên máy ARM (arm64, aarch64). Bộ cài nhận máy aarch64 và tải `mpn_linux_arm64`. Module PageSpeed và ModSecurity cho arm64 lấy từ bản [nginx-1.28.3-r2](https://github.com/marixdev/mpn-packages/releases/tag/nginx-1.28.3-r2) (build trên runner ARM của GitHub, ghim SHA-256 theo từng hệ điều hành và kiến trúc). Web mail lên [mpn-mail 1.0.2](https://github.com/marixdev/mpn-mail/releases/tag/v1.0.2), có bản `mpn-mail_linux_arm64`.
- Bật mail tự gỡ các máy chủ thư có sẵn sẽ chiếm cổng 25 (Exim, Sendmail, OpenSMTPD) trước khi cài Postfix, thay vì dừng lại và bắt gỡ bằng tay.

### Sửa

- Mail bật được trên Debian 13 (và Ubuntu 26.04): panel nhận ra Dovecot 2.4 và viết `dovecot.conf` theo cú pháp mới (`dovecot_config_version`, `mail_driver`, hạn mức `quota_storage_size`). Dovecot 2.3 giữ cấu hình cũ.
- Bước gói cơ bản cài `cron` và bật nó: một số bản Debian 13 không có sẵn, nên tab Cron không chạy được việc nào.

## [1.0.2] - 2026-11-12

### Thêm

- Trình quản lý tệp có Cắt, Sao chép và Dán, trong menu chuột phải, trên thanh khi chọn nhiều mục và bằng phím Ctrl+X, Ctrl+C, Ctrl+V. Mục đã cắt hoặc sao chép được nhớ theo từng website trong thẻ trình duyệt đang mở, nên có thể mở thư mục khác rồi dán vào đó. Bấm chuột phải lên một thư mục sẽ dán vào chính thư mục ấy.
- Sao chép vào chính thư mục đang chứa mục gốc thì bản sao được đặt tên `tên (copy).đuôi`, `tên (copy 2).đuôi`, v.v. Sao chép giữ quyền của tệp gốc, còn liên kết và tệp đặc biệt được bỏ qua.

### An toàn

- Dán không bao giờ ghi đè: nếu tên đã có ở thư mục đích thì không có mục nào bị chuyển hoặc chép. Không dán được một thư mục vào chính nó hay thư mục con của nó, và không đi ra ngoài thư mục home của website.

## [1.0.1] - 2026-11-12

### Thêm

- Giao diện tiếng Trung giản thể (`zh`), cạnh tiếng Việt và tiếng Anh. Chọn ở nút ngôn ngữ trên thanh trên cùng, kể cả ở trang đăng nhập và `/setup`; lần đầu vào, panel chọn theo `Accept-Language` của trình duyệt.
- Phông chữ có thêm PingFang SC, Microsoft YaHei và Noto Sans CJK SC, vì JetBrains Mono không có chữ Hán.

### Thay đổi

- Web mail lên [mpn-mail 1.0.1](https://github.com/marixdev/mpn-mail/releases/tag/v1.0.1), bản có tiếng Trung và đi theo ngôn ngữ của panel.
- Mô tả các thiết lập PHP trong tab Tổng quan nay hiện theo ngôn ngữ đang chọn, trước đây luôn là tiếng Việt.
- README có thêm bản tiếng Anh và tiếng Trung ([README.zh.md](README.zh.md)), kèm bảng các repo đi kèm (mpn-mail, mpn-nginx-modules).

## [1.0.0] - 2026-11-12

Bản phát hành đầu tiên: website PHP, Node.js, Python, tĩnh và proxy; SSL Let's Encrypt; MariaDB/MySQL và phpMyAdmin; email và web mail mpn-mail 1.0.0; sao lưu; tường lửa, ModSecurity, fail2ban, 2FA; giám sát; giao diện tiếng Việt và tiếng Anh.

[1.2.0]: https://github.com/marixdev/mini-panel/compare/v1.1.1...v1.2.0
[1.1.1]: https://github.com/marixdev/mini-panel/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/marixdev/mini-panel/compare/v1.0.3...v1.1.0
[1.0.3]: https://github.com/marixdev/mini-panel/compare/v1.0.2...v1.0.3
[1.0.2]: https://github.com/marixdev/mini-panel/compare/v1.0.1...v1.0.2
[1.0.1]: https://github.com/marixdev/mini-panel/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/marixdev/mini-panel/releases/tag/v1.0.0
