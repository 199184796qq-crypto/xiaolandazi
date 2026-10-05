package db

import (
	"context"
	"strings"
)

func (s *Store) SaveWechatNativeCodeURL(ctx context.Context, paymentID int64, codeURL string) error {
	if len(codeURL) > 512 || !strings.HasPrefix(codeURL, "weixin://") || strings.ContainsAny(codeURL, "\r\n") {
		return ErrWechatPaymentMismatch
	}
	result, err := s.db.ExecContext(ctx, `UPDATE fin_payment_transactions SET provider_code_url=?,provider_trade_state='NOTPAY'
	 WHERE id=? AND channel='wechat' AND payment_method='wechat_native' AND status='pending'
	 AND (provider_code_url='' OR provider_code_url=?)`, codeURL, paymentID, codeURL)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var existing string
		if err := s.db.QueryRowContext(ctx, `SELECT provider_code_url FROM fin_payment_transactions WHERE id=? AND channel='wechat' AND payment_method='wechat_native' AND status='pending'`, paymentID).Scan(&existing); err != nil || existing != codeURL {
			return ErrWechatPaymentNotPending
		}
	}
	return nil
}
