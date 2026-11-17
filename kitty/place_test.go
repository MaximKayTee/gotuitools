package kitty

import "testing"

func TestPlace(t *testing.T) {
	tests := []struct {
		name                 string
		id, pid              uint32
		col, row, cols, rows int
		sx, sy, sw, sh, z    int
		want                 string
	}{
		{
			"origin whole image", 7, 1, 0, 0, 80, 23, 0, 0, 0, 0, 0,
			"\x1b7\x1b[1;1H\x1b_Ga=p,i=7,p=1,c=80,r=23,x=0,y=0,w=0,h=0,z=0,C=1,q=2\x1b\\\x1b8",
		},
		{
			"offset with source rect", 0x01000002, 1, 12, 3, 40, 20, 100, 50, 640, 320, -1,
			"\x1b7\x1b[4;13H\x1b_Ga=p,i=16777218,p=1,c=40,r=20,x=100,y=50,w=640,h=320,z=-1,C=1,q=2\x1b\\\x1b8",
		},
		{
			"max ids", 4294967295, 4294967295, 299, 99, 1, 1, 0, 0, 1, 1, 5,
			"\x1b7\x1b[100;300H\x1b_Ga=p,i=4294967295,p=4294967295,c=1,r=1,x=0,y=0,w=1,h=1,z=5,C=1,q=2\x1b\\\x1b8",
		},
		{
			"negatives clamp, z kept", 3, 2, -4, -1, -2, -3, -5, -6, -7, -8, -1073741825,
			"\x1b7\x1b[1;1H\x1b_Ga=p,i=3,p=2,c=0,r=0,x=0,y=0,w=0,h=0,z=-1073741825,C=1,q=2\x1b\\\x1b8",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Place(tt.id, tt.pid, tt.col, tt.row, tt.cols, tt.rows, tt.sx, tt.sy, tt.sw, tt.sh, tt.z)
			if got != tt.want {
				t.Fatalf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestDeletePlacement(t *testing.T) {
	tests := []struct {
		id, pid uint32
		want    string
	}{
		{42, 1, "\x1b_Ga=d,d=i,i=42,p=1,q=2\x1b\\"},
		{4294967295, 16777215, "\x1b_Ga=d,d=i,i=4294967295,p=16777215,q=2\x1b\\"},
		{1, 0, "\x1b_Ga=d,d=i,i=1,p=0,q=2\x1b\\"},
	}
	for _, tt := range tests {
		if got := DeletePlacement(tt.id, tt.pid); got != tt.want {
			t.Errorf("DeletePlacement(%d, %d) = %q, want %q", tt.id, tt.pid, got, tt.want)
		}
	}
}
