package service

import "context"

// CampaignRankRow is one campaign leaderboard row.
type CampaignRankRow struct {
	CreatorID uint64 `json:"creator_id"`
	Points    int64  `json:"points"`
}

// GetLiveContributors returns top contributors for a live session.
func (s *Service) GetLiveContributors(ctx context.Context, liveSessionID uint64, limit int) ([]ContributorSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT viewer_id, SUM(charged_coins) AS total
		FROM gift_orders
		WHERE live_session_id = ? AND status = 'ACCEPTED'
		GROUP BY viewer_id
		ORDER BY total DESC, viewer_id ASC
		LIMIT ?
	`, liveSessionID, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]ContributorSummary, 0, limit)
	for rows.Next() {
		var c ContributorSummary
		if err := rows.Scan(&c.ViewerID, &c.Coins); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCampaignLeaderboard returns top creators for a campaign.
func (s *Service) GetCampaignLeaderboard(ctx context.Context, campaignID uint64, limit int) ([]CampaignRankRow, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT creator_id, COALESCE(SUM(points), 0) AS total
		FROM campaign_point_ledger
		WHERE campaign_id = ?
		GROUP BY creator_id
		ORDER BY total DESC, creator_id ASC
		LIMIT ?
	`, campaignID, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]CampaignRankRow, 0, limit)
	for rows.Next() {
		var row CampaignRankRow
		if err := rows.Scan(&row.CreatorID, &row.Points); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
