package wxaiinspection

import "math/rand"

func pickWxaiSample(accounts []account, sampleSize int) []account {
	if sampleSize <= 0 || sampleSize >= len(accounts) {
		selectedAccounts := make([]account, len(accounts))
		copy(selectedAccounts, accounts)
		return selectedAccounts
	}
	selectedAccounts := make([]account, len(accounts))
	copy(selectedAccounts, accounts)
	rand.Shuffle(len(selectedAccounts), func(leftIndex int, rightIndex int) {
		selectedAccounts[leftIndex], selectedAccounts[rightIndex] = selectedAccounts[rightIndex], selectedAccounts[leftIndex]
	})
	return selectedAccounts[:sampleSize]
}
