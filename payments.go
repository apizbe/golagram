package golagram

import (
	"context"
	"fmt"
)

// StarsCurrency is the currency code for Telegram Stars payments.
const StarsCurrency = "XTR"

// NewStarsInvoice builds a sendInvoice request for a Telegram Stars
// payment: currency XTR, no provider token, exactly one price component
// (all three are what the Stars flow requires). Tweak the returned request
// (photo, reply markup, ...) before sending:
//
//	req := gg.NewStarsInvoice(gg.ChatIDFromInt(chatID), "Pro", "Unlock pro features", "pro-1m", 250)
//	req.PhotoURL = "https://example.com/pro.png"
//	msg, err := bot.SendInvoice(ctx, req)
func NewStarsInvoice(chatID ChatID, title, description, payload string, stars int64) *SendInvoiceRequest {
	return &SendInvoiceRequest{
		ChatID:      chatID,
		Title:       title,
		Description: description,
		Payload:     payload,
		Currency:    StarsCurrency,
		Prices:      []LabeledPrice{{Label: title, Amount: stars}},
	}
}

// SendStarsInvoice sends a Telegram Stars invoice into whichever chat this
// update relates to. For anything beyond the required fields, build the
// request with [NewStarsInvoice] and send it via [TelegramBot.SendInvoice].
func (c *Ctx) SendStarsInvoice(title, description, payload string, stars int64) (*Message, error) {
	chat := c.Chat()
	if chat == nil {
		return nil, fmt.Errorf("Ctx.SendStarsInvoice: this update has no chat to send into")
	}
	return c.Bot().SendInvoice(c, NewStarsInvoice(ChatIDFromInt(chat.ID), title, description, payload, stars))
}

// AnswerPreCheckout approves this update's pre-checkout query, letting the
// payment complete. Telegram requires an answer within 10 seconds of the
// query — do stock checks fast or approve optimistically and refund.
func (c *Ctx) AnswerPreCheckout() error {
	return c.answerPreCheckout(true, "")
}

// AnswerPreCheckoutError declines this update's pre-checkout query with a
// human-readable reason Telegram shows to the user (e.g. "Sorry, we just
// sold out").
func (c *Ctx) AnswerPreCheckoutError(reason string) error {
	return c.answerPreCheckout(false, reason)
}

func (c *Ctx) answerPreCheckout(ok bool, reason string) error {
	if c.PreCheckoutQuery == nil {
		return fmt.Errorf("Ctx.AnswerPreCheckout: this update is not a pre_checkout_query")
	}
	_, err := c.Bot().AnswerPreCheckoutQuery(c, &AnswerPreCheckoutQueryRequest{
		PreCheckoutQueryID: c.PreCheckoutQuery.ID,
		Ok:                 ok,
		ErrorMessage:       reason,
	})
	return err
}

// AnswerShipping approves this update's shipping query with the available
// shipping options (for invoices sent with is_flexible).
func (c *Ctx) AnswerShipping(options []ShippingOption) error {
	return c.answerShipping(true, options, "")
}

// AnswerShippingError declines this update's shipping query with a
// human-readable reason Telegram shows to the user (e.g. "Sorry, we don't
// deliver to your region").
func (c *Ctx) AnswerShippingError(reason string) error {
	return c.answerShipping(false, nil, reason)
}

func (c *Ctx) answerShipping(ok bool, options []ShippingOption, reason string) error {
	if c.ShippingQuery == nil {
		return fmt.Errorf("Ctx.AnswerShipping: this update is not a shipping_query")
	}
	_, err := c.Bot().AnswerShippingQuery(c, &AnswerShippingQueryRequest{
		ShippingQueryID: c.ShippingQuery.ID,
		Ok:              ok,
		ShippingOptions: options,
		ErrorMessage:    reason,
	})
	return err
}

// RefundStars refunds a completed Telegram Stars payment. The charge ID
// comes from [SuccessfulPayment.TelegramPaymentChargeID] — persist it at
// payment time; it's the only handle for a later refund.
func (b *TelegramBot) RefundStars(ctx context.Context, userID int64, telegramPaymentChargeID string) error {
	_, err := b.RefundStarPayment(ctx, &RefundStarPaymentRequest{
		UserID:                  userID,
		TelegramPaymentChargeID: telegramPaymentChargeID,
	})
	return err
}
