---
title: "Tài liệu của bạn cũng là một con voi. Vấn đề là nó nhớ cả những điều đã sai."
date: 2026-11-03T09:00:00+07:00
draft: false
author: Huy Nguyễn Việt
---
Tôi bắt đầu với một lời thú nhận: tôi đã mệt mỏi với phát triển hướng đặc tả (spec-driven development).
<!--more-->

Ý tưởng thì đẹp. Viết đặc tả trước, code sau, để AI có đường ray mà chạy. Nhưng thực tế tôi gặp thường như thế này: gõ một yêu cầu, và vài giây sau có ngay một tài liệu dài ba mươi trang. Đầy đủ mục, đầy đủ tiêu đề, và không ai đọc nổi. Chúng ta đã đi thẳng từ *yêu cầu* đến *tài liệu*, bỏ qua mọi bước ở giữa. Mà những bước ở giữa chính là nơi rủi ro được gỡ ra.

Rồi tôi gặp EGM.

## Voi và cá vàng

Elephant-Goldfish Model (EGM) là phương pháp của Dave Rensin, Distinguished Engineer tại Google. Ý tưởng khá đơn giản:

- **Con voi** là một phiên làm việc dài với AI. Nó nhớ mọi thứ: kiến trúc, ý định, những quyết định nhỏ. Nó không viết code. Nó phỏng vấn bạn, tranh luận với bạn, và giữ biên bản.
- **Con cá vàng** là một phiên AI mới tinh, không có trí nhớ. Nó chỉ được đọc tài liệu thiết kế. Nếu nó hiểu sai kế hoạch, thì lỗi không nằm ở con cá. Tài liệu chưa đủ rõ.

Quy trình đi qua ba chặng: brainstorm, draft, review (động não, viết nháp, phản biện). Chỉ khi cá vàng đọc xong mà hiểu đúng, code mới được phép bắt đầu. Rensin gọi gọn: *"Design is the new code"* (thiết kế chính là code mới). Khi AI viết code nhanh hơn người đọc, thứ con người thật sự cần kiểm soát là bản thiết kế.

Với tôi, điều làm EGM khác biệt là nó **ép đi chậm đúng chỗ**. Không có tài liệu nào ra đời chỉ vì ai đó bấm Enter. Nó phải sống sót qua cuộc thẩm vấn của những con cá vàng không có lý do gì để nể mặt bạn.

Dự án Vanguard của tôi được dựng bằng chính quy trình đó: brainstorm, draft, review, rồi lại review. Tôi nhận ra là nó đi rất nhanh, vì mỗi bước nhỏ đều đã bị thử thách trước khi bước tiếp theo bắt đầu. Nên bài này không chỉ giới thiệu một công cụ. Nó là lời cảm ơn, và là một ý tưởng nối tiếp mà tôi muốn đóng góp lại cho EGM.

## Nỗi đau mới

EGM giải quyết rất tốt một câu hỏi: *tài liệu này có đủ tốt để bắt đầu không?*

Nhưng sống với EGM đủ lâu, bạn sẽ gặp câu hỏi tiếp theo, và nó khó chịu hơn nhiều:

> **Tài liệu này có còn đúng không?**

Hãy tưởng tượng sáu tháng sau. Bạn có vài trăm file markdown. Các module được sửa mỗi ngày. Mười người trong nhóm, mỗi người đụng vào một góc khác nhau. Có một file `payments-spec.md`. Ai chịu trách nhiệm về nó? Lần cuối có người thật sự đọc lại nó là khi nào? Code trong `src/payments/` đã đổi bao nhiêu lần kể từ đó?

Không ai biết. Và đây là phần tệ nhất: **không có gì cảnh báo cả.**

Bạn có thể nghĩ git đã lo chuyện này. Tài liệu nằm cạnh code, cùng repo, cùng lịch sử. Nhưng nằm cạnh nhau không có nghĩa là khớp nhau. Git nhớ rất rõ cả hai file đã thay đổi. Nó không biết, và không có lý do để biết, rằng file này đang mô tả file kia. Sự gần gũi chỉ tạo ra cảm giác an toàn, và cảm giác ấy chưa bao giờ được kiểm chứng.

Các công cụ phát triển hướng đặc tả hiện nay cũng chưa giải quyết được phần này. OpenSpec giúp việc trôi lệch ít xảy ra hơn *nếu mọi người tuân thủ quy trình*, nhưng một lần sửa code thẳng tay, bỏ qua thư mục thay đổi, là không còn chuông báo nào kêu. Các bước quét độ lệch thì thuộc kiểu "có người nhớ chạy thì mới biết". Mà nếu có thứ gì con người hay quên, thì đó là việc đi kiểm tra một thứ trông vẫn ổn.

Nói cách khác: con voi nhớ rất giỏi. Nhưng tài liệu là một con voi nhớ cả những điều đã sai, và nó nhớ chúng với vẻ tự tin y hệt như điều đúng.

## Vanguard: người gõ cửa

Vanguard là một công cụ dòng lệnh nhỏ, đứng một mình, chuyên quản lý vòng đời trạng thái của tài liệu. Nó trả lời đúng một câu hỏi, và trả lời một cách xác định (cùng đầu vào luôn cho cùng đầu ra):

> **Tài liệu nào đã lỗi thời, và chính xác điều gì khiến nó lỗi thời?**

Không có mô hình ngôn ngữ nào bên trong. Không tiến trình chạy ngầm. Không cần server. Cùng đầu vào, cùng đầu ra, mỗi lần.

Nguyên lý hoạt động khá đơn giản. Mỗi tài liệu khai báo nó đang "trông chừng" phần code nào:

```yaml
---
id: payments-spec
schema: feature-spec
watches:
  paths: ["src/payments/**"]
  review_after: 90d
---
```

Một con người đọc tài liệu, đối chiếu với code, thấy nó đúng, rồi chạy `vanguard observe payments-spec`. Đó là lời xác nhận: *"Tôi đã xem xét, tài liệu này chính xác tại commit này."* Lời xác nhận được ghi vào một file duy nhất, `vanguard.lock`, và được commit như `go.sum`. Từ đó, nếu code trong `src/payments/` thay đổi, hoặc quá 90 ngày chưa ai xem lại, hoặc chính tài liệu bị sửa mà chưa được xác nhận lại, thì tài liệu chuyển sang trạng thái **lỗi thời**, kèm theo chính xác commit nào, đường dẫn nào, và lý do nào gây ra điều đó.

Dùng hằng ngày chỉ có vài lệnh:

```text
vanguard init                                      # dựng thư mục .vanguard/
vanguard observe payments-spec                     # "tôi đã duyệt, nó đúng"
vanguard check                                     # tài liệu nào đã lỗi thời, vì sao?
vanguard check --path src/payments --format json   # agent hỏi trước khi sửa code
```

Dòng cuối là lý do tôi hào hứng nhất. Trước khi động vào module thanh toán, một AI agent có thể hỏi: "Có tài liệu nào đang mô tả chỗ này mà đã lỗi thời không?" Và nhận về JSON có cấu trúc, cùng mã thoát (exit code) để hệ thống CI dùng được ngay.

## Câu trả lời của Vanguard có ý nghĩa đến đâu

Đây là phần quan trọng nhất của bài, nên tôi nói thẳng.

**Vanguard quản lý vòng đời trạng thái của tài liệu. Nó không thể cho bạn biết tài liệu có còn đúng hay không.** Không công cụ nào đo bằng mã băm và commit làm được điều đó. Nó chỉ biết hai thứ: *lần cuối có người xác nhận là khi nào*, và *kể từ đó, những gì tài liệu đang trông chừng đã đổi chưa*.

Vì vậy hãy đọc hai trạng thái này cho đúng:

- **Còn mới** không có nghĩa là "đúng". Nó có nghĩa là: *một con người đã duyệt, và kể từ đó không có gì mà tài liệu theo dõi bị thay đổi.*
- **Lỗi thời** không có nghĩa là "sai". Nó có nghĩa là: *nền tảng của lời xác nhận trước đó đã đổi.* Rất có thể code chỉ được tái cấu trúc, tài liệu vẫn đúng từng chữ. Vanguard cho bạn biết *nó lỗi thời và vì sao*, còn việc nó có thật sự sai hay không là phán đoán của con người.

Điều đó dẫn đến hai hệ quả.

**Thứ nhất, việc đánh dấu một tài liệu là mới hay lỗi thời chỉ có ý nghĩa khi nó đi qua bước duyệt của con người.** Nếu ai đó chạy `observe` mà không thực sự đọc, Vanguard không bắt được. Cái nó làm được là biến lời xác nhận ấy thành một dòng thay đổi trong git, để người review nhìn thấy, chất vấn, và truy ra ai đã nói "đúng". Máy ghi lại trách nhiệm. Con người mới là bên gánh trách nhiệm.

**Thứ hai, bạn vẫn cần các kỹ năng phát triển hướng đặc tả khác như EGM để làm phần kiểm tra thật sự.** Khi Vanguard báo một tài liệu đã lỗi thời, đó mới là lúc việc thật bắt đầu: đối chiếu tài liệu với code hiện tại, quyết định sửa gì, và kiểm tra lại xem bản sửa đã đủ rõ chưa. Đó chính là đất của EGM: con voi giữ ngữ cảnh để cùng bạn rà lại ý định, cá vàng đọc lại bản cập nhật bằng con mắt không thiên kiến, và những người phản biện tìm ra chỗ hở. Vanguard không thay thế bất kỳ bước nào trong số đó. Nó cho bạn biết *tài liệu nào cần được đưa vào quy trình ấy, và vì sao.*

Vòng lặp đầy đủ trông như thế này:

1. Code thay đổi, được commit.
2. Vanguard báo: `payments-spec` đã lỗi thời, và đây là các commit khiến nó lỗi thời.
3. Con người cùng EGM rà lại: tài liệu còn đúng không? Cần sửa gì? Một con cá vàng mới đọc lại có hiểu không?
4. Sửa tài liệu, commit.
5. Con người chạy `observe`: một lời xác nhận mới.
6. Commit file lock. Lời xác nhận trở thành một dấu vết lịch sử có thể review.

Máy lo việc đo đạc và báo cáo. Con người, được EGM hỗ trợ, lo việc phán đoán.

## Năm nguyên tắc

1. **Chỉ "mới" khi có người xác nhận.** Trạng thái lỗi thời chỉ được xóa bởi một lần `observe` của con người, được commit vào git. Sửa tài liệu thôi là chưa đủ.
2. **Không bao giờ im lặng khi sai.** Mất file lock, link gãy, file hỏng: luôn báo đỏ.
3. **Một câu trả lời xác định.** Tài liệu nào đã lỗi thời, và commit nào gây ra.
4. **Không áp đặt ý kiến.** Công cụ chỉ sở hữu danh tính tài liệu, các điều kiện theo dõi và file lock. Bố cục, lược đồ, quy tắc trình bày là của bạn — engine chỉ ràng buộc đúng cái *bạn* đã tuyên bố.
5. **Đầu ra dành cho máy.** Gói JSON và mã thoát cho CI và agent. Công cụ tự giải thích qua `--help` và qua chính thông báo lỗi.

Và nguyên tắc thứ sáu, cũng là nguồn gốc của nó: **EGM dẫn dắt quy trình.**

## Ghép lại

Nếu EGM trả lời câu hỏi *"tài liệu này có đủ tốt để bắt đầu không?"*, thì Vanguard cho bạn biết *tài liệu nào đã lỗi thời, và điều gì khiến nó lỗi thời*, để bạn biết khi nào cần hỏi lại câu đó.

Cá vàng kiểm tra lúc tài liệu ra đời, và kiểm tra lại mỗi khi nó được cập nhật. Vanguard gõ cửa khi nền tảng của lần kiểm tra trước không còn đứng vững. Hai thứ này không thay thế nhau. Chúng là hai nửa của cùng một vòng đời.

Dave Rensin đã nói rằng thiết kế là code mới. Tôi chỉ muốn thêm một vế: **thiết kế đã lỗi thời mà không ai biết thì còn tệ hơn không có thiết kế.**

## Tải về

Vanguard là mã nguồn mở Apache-2.0, viết bằng Go, không phụ thuộc runtime. Tải binary tại [trang releases](https://github.com/vanguard-lint/vanguard-spec/releases) (v0.2.0 vừa phát hành tuần này), đọc [tài liệu thiết kế](https://github.com/vanguard-lint/vanguard-spec/blob/main/docs/designs/vanguard.md), hoặc xem [mã nguồn](https://github.com/vanguard-lint/vanguard-spec) — engine tự kiểm tra store của chính nó ở mỗi commit, nên README bạn đọc ở đó đã được xác nhận còn tươi.
